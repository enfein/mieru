// Copyright (C) 2024  mieru authors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package congestion

import (
	"fmt"
	mrand "math/rand"
	"sync"
	"time"

	"github.com/enfein/mieru/v3/pkg/log"
	"github.com/enfein/mieru/v3/pkg/mathext"
)

type bbrMode int

const (
	// Startup phase of the connection.
	modeStartUp bbrMode = iota

	// After achieving the highest possible bandwidth during the startup, lower
	// the pacing rate in order to drain the queue.
	modeDrain

	// Cruising mode.
	modeProbeBW

	// Temporarily slow down sending in order to empty the buffer and measure
	// the real minimum RTT.
	modeProbeRTT
)

// Indicates how the congestion control limits the amount of bytes in flight.
type bbrRecoveryState int

const (
	// Do not limit.
	stateNotInRecovery bbrRecoveryState = iota

	// Allow 1 extra outstanding byte for each byte acknowledged.
	stateConservation

	// Allow 2 extra outstanding bytes for each byte acknowledged (slow start).
	stateGrowth
)

const (
	timeFormat = "15:04:05.999"

	maxDatagramSize = 1500

	defaultMinimumPacingRate = 64 * 1024 // 64 KBps

	// The minimum CWND to ensure delayed acks don't reduce bandwidth measurements.
	// Does not inflate the pacing rate.
	defaultMinimumCongestionWindow = 16 * maxDatagramSize

	defaultInitialCongestionWindow = 32 * maxDatagramSize

	defaultMaximumCongestionWindow = 4096 * maxDatagramSize

	// The gain used for the slow start, equal to 2/ln(2).
	highGain = 2.885

	// The gain used in STARTUP after loss has been detected.
	// 1.5 is enough to allow for 25% exogenous loss and still observe a 25% growth
	// in measured bandwidth.
	StartupAfterLossGain = 1.5

	// The gain used to drain the queue after the slow start.
	drainGain = 1.0 / highGain

	// The length of the gain cycle.
	gainCycleLength = 8

	// The size of the bandwidth filter window, in round-trips.
	bandwidthFilterWindowSize = gainCycleLength + 2

	// The time after which the current min RTT value expires.
	minRTTExpiry = 10 * time.Second

	// The minimum time the connection can spend in PROBE RTT mode.
	probeRTTTime = 200 * time.Millisecond

	// If the bandwidth does not increase by the factor of startUpGrowthTarget
	// within roundTripsWithoutGrowthBeforeExitingStartup rounds, the connection
	// will exit the STARTUP mode.
	startUpGrowthTarget = 1.25

	roundTripsWithoutGrowthBeforeExitingStartup = 3
)

var (
	// The cycle of gains used during the PROBE BW stage.
	pacingGainList = [8]float64{1.25, 0.75, 1.0, 1.0, 1.0, 1.0, 1.0, 1.0}
)

type AckedPacketInfo struct {
	PacketNumber     int64
	BytesAcked       int64
	ReceiveTimestamp time.Time
}

func (i AckedPacketInfo) String() string {
	return fmt.Sprintf("AckedPacketInfo{PacketNumber=%d, BytesAcked=%d, ReceiveTimestamp=%s}", i.PacketNumber, i.BytesAcked, i.ReceiveTimestamp.Format(timeFormat))
}

type LostPacketInfo struct {
	PacketNumber int64
	BytesLost    int64
}

func (i LostPacketInfo) String() string {
	return fmt.Sprintf("LostPacketInfo{PacketNumber=%d, BytesLost=%d}", i.PacketNumber, i.BytesLost)
}

type BBRSender struct {
	mu sync.Mutex

	// Additional context of this BBRSender. Used in the log.
	loggingContext string

	rttStats *RTTStats

	pacer *Pacer

	// Replaces unacked_packets_->bytes_in_flight().
	bytesInFlight int64

	// Current BBR running mode.
	mode bbrMode

	// Bandwidth sampler provides BBR with the bandwidth measurements at
	// individual points.
	sampler BandwidthSamplerInterface

	// The number of the round trips that have occurred during the connection.
	roundTripCount int64

	// The packet number of the most recently sent packet.
	lastSentPacket int64

	// Acknowledgement of any packet after currentRoundTripEnd will cause
	// the round trip counter to advance.
	currentRoundTripEnd int64

	// Tracks the maximum bandwidth over the multiple recent round-trips.
	maxBandwidth *WindowedFilter[int64]

	// Tracks the maximum number of bytes acked faster than the sending rate.
	maxAckHeight *WindowedFilter[int64]

	// The time this aggregation started and the number of bytes acked during it.
	aggregationEpochStartTime time.Time
	aggregationEpochBytes     int64

	// The number of bytes acknowledged since the last time bytes in flight
	// dropped below the target window.
	bytesAckedSinceQueueDrained int64

	// The muliplier for calculating the max amount of extra CWND to add to
	// compensate for ack aggregation.
	maxAggregationBytesMultiplier float64

	// Minimum RTT estimate. Automatically expires within 10 seconds
	// and triggers PROBE RTT mode if no new value is sampled
	// during that period.
	minRTT time.Duration

	// The time at which the current value of minRTT was assigned.
	minRTTTimestamp time.Time

	// The maximum allowed number of bytes in flight.
	congestionWindow int64

	// The initial value of the congestionWindow.
	initialCongestionWindow int64

	// The largest value the congestionWindow can achieve.
	maxCongestionWindow int64

	// The smallest value the congestionWindow can achieve.
	minCongestionWindow int64

	// The current pacing rate of the connection.
	pacingRate int64

	// The gain currently applied to the pacing rate.
	pacingGain float64

	// The gain currently applied to the congestion window.
	congestionWindowGain float64

	// The gain used for the congestion window during PROBE BW.
	congestionWindowGainConstant float64

	// The coefficient by which mean RTT variance is added to the congestion window.
	rttVarianceWeight float64

	// The number of RTTs to stay in STARTUP mode.
	numStartupRTTs int64

	// If true, exit startup if 1 RTT has passed with no bandwidth increase and
	// the connection is in recovery.
	exitStartupOnLoss bool

	// Number of round-trips in PROBE BW mode, used for determining the current
	// pacing gain cycle.
	cycleCurrentOffset int

	// The time at which the last pacing gain cycle was started.
	lastCycleStart time.Time

	// Indicates whether the connection has reached the full bandwidth mode.
	isAtFullBandwidth bool

	// Number of rounds during which there was no significant bandwidth increase.
	roundsWithoutBandwidthGain int64

	// The bandwidth compared to which the increase is measured.
	bandwidthAtLastRound int64

	// Set to true upon exiting quiescence. Quiescence means bytesInFlight is
	// zero and app is not sending data.
	exitingQuiescence bool

	// Time at which PROBE RTT has to be exited. Setting it to zero indicates
	// that the time is yet unknown as the number of packets in flight has not
	// reached the required value.
	exitProbeRTTAt time.Time

	// Indicates whether a round-trip has passed since PROBE RTT became active.
	probeRTTRoundPassed bool

	// Indicates whether the most recent bandwidth sample was marked as
	// app-limited.
	lastSampleIsAppLimited bool

	// Current state of recovery.
	recoveryState bbrRecoveryState

	// Receiving acknowledgement of a packet after endRecoveryAt will cause
	// BBR to exit the recovery mode. A value above zero indicates at least one
	// loss has been detected, so it must not be set back to zero.
	endRecoveryAt int64

	// A window used to limit the number of bytes in flight during loss recovery.
	recoveryWindow int64

	// When true, recovery is rate based rather than congestion window based.
	rateBasedRecovery bool

	// When true, pace at 1.5x and disable packet conservation in STARTUP.
	slowerStartup bool

	// When true, disables packet conservation in STARTUP.
	rateBasedStartup bool

	// Used as the initial packet conservation mode when first entering recovery.
	initialConservationInStartup bbrRecoveryState

	appLimitedSinceLastProbeRTT bool
	minRTTSinceLastProbeRTT     time.Duration
}

// NewBBRSender constructs a new BBR sender object.
func NewBBRSender(loggingContext string, rttStats *RTTStats) *BBRSender {
	s := &BBRSender{
		loggingContext:               loggingContext,
		pacer:                        NewPacer(defaultInitialCongestionWindow, defaultMaximumCongestionWindow, defaultMinimumPacingRate),
		mode:                         modeStartUp,
		sampler:                      NewBandwidthSampler(),
		maxBandwidth:                 NewWindowedFilter(bandwidthFilterWindowSize, 0, MaxFilter[int64]),
		maxAckHeight:                 NewWindowedFilter(bandwidthFilterWindowSize, 0, MaxFilter[int64]),
		congestionWindow:             defaultInitialCongestionWindow,
		initialCongestionWindow:      defaultInitialCongestionWindow,
		maxCongestionWindow:          defaultMaximumCongestionWindow,
		minCongestionWindow:          defaultMinimumCongestionWindow,
		pacingGain:                   1.0,
		congestionWindowGain:         1.0,
		congestionWindowGainConstant: 2.0,
		rttVarianceWeight:            0.0,
		numStartupRTTs:               roundTripsWithoutGrowthBeforeExitingStartup,
		recoveryState:                stateNotInRecovery,
		recoveryWindow:               defaultMaximumCongestionWindow,
		initialConservationInStartup: stateConservation,
		minRTTSinceLastProbeRTT:      infDuration,
	}
	if rttStats != nil {
		s.rttStats = rttStats
	} else {
		s.rttStats = NewRTTStats()
	}
	return s
}

// OnPacketSent updates BBR sender state when a packet is being sent.
func (b *BBRSender) OnPacketSent(sentTime time.Time, bytesInFlight int64, packetNumber int64, bytes int64, hasRetransmittableData bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.lastSentPacket = packetNumber
	b.bytesInFlight = bytesInFlight

	if bytesInFlight <= 0 && b.sampler.IsAppLimited() {
		b.exitingQuiescence = true
	}

	if b.aggregationEpochStartTime.IsZero() {
		b.aggregationEpochStartTime = sentTime
	}

	b.sampler.OnPacketSent(sentTime, packetNumber, bytes, bytesInFlight, hasRetransmittableData)
	b.pacer.OnPacketSent(sentTime, bytes, b.getPacingRate())
	if log.IsLevelEnabled(log.TraceLevel) {
		log.Tracef("[BBRSender %s] OnPacketSent(bytesInFlight=%d, packetNumber=%d, bytes=%d), pacingRate=%d => pacingBudget=%d", b.loggingContext, bytesInFlight, packetNumber, bytes, b.getPacingRate(), b.pacer.Budget(sentTime, b.getPacingRate()))
	}
}

// OnCongestionEvent updates BBR sender state from acknowledged and lost packets.
func (b *BBRSender) OnCongestionEvent(priorInFlight int64, eventTime time.Time, ackedPackets []AckedPacketInfo, lostPackets []LostPacketInfo) {
	b.mu.Lock()
	defer b.mu.Unlock()

	totalBytesAckedBefore := b.sampler.TotalBytesAcked()
	isRoundStart := false
	isMinRTTExpired := false

	b.bytesInFlight = priorInFlight
	for _, p := range ackedPackets {
		b.bytesInFlight -= p.BytesAcked
	}
	for _, p := range lostPackets {
		b.bytesInFlight -= p.BytesLost
	}
	b.bytesInFlight = mathext.Max(b.bytesInFlight, 0)

	b.discardLostPackets(lostPackets)

	// Input the new data into the BBR model of the connection.
	if len(ackedPackets) > 0 {
		lastAckedPacket := ackedPackets[len(ackedPackets)-1].PacketNumber
		isRoundStart = b.updateRoundTripCounter(lastAckedPacket)
		isMinRTTExpired = b.updateBandwidthAndMinRTT(eventTime, ackedPackets)
		b.updateRecoveryState(lastAckedPacket, len(lostPackets) > 0, isRoundStart)

		bytesAcked := b.sampler.TotalBytesAcked() - totalBytesAckedBefore

		b.updateAckAggregationBytes(eventTime, bytesAcked)
		if b.maxAggregationBytesMultiplier > 0 {
			if b.bytesInFlight <= int64(1.25*float64(b.getTargetCongestionWindow(b.pacingGain))) {
				b.bytesAckedSinceQueueDrained = 0
			} else {
				b.bytesAckedSinceQueueDrained += bytesAcked
			}
		}
	}

	// Handle logic specific to PROBE BW mode.
	if b.mode == modeProbeBW {
		b.updateGainCyclePhase(eventTime, priorInFlight, len(lostPackets) > 0)
	}

	// Handle logic specific to STARTUP and DRAIN modes.
	if isRoundStart && !b.isAtFullBandwidth {
		b.checkIfFullBandwidthReached()
	}
	b.maybeExitStartupOrDrain(eventTime)

	// Handle logic specific to PROBE RTT.
	b.maybeEnterOrExitProbeRTT(eventTime, isRoundStart, isMinRTTExpired)

	// Calculate number of packets acked and lost.
	bytesAcked := b.sampler.TotalBytesAcked() - totalBytesAckedBefore
	var bytesLost int64
	for _, lost := range lostPackets {
		bytesLost += lost.BytesLost
	}

	// After the model is updated, recalculate the pacing rate and congestion
	// window.
	b.calculatePacingRate()
	b.calculateCongestionWindow(bytesAcked)
	b.calculateRecoveryWindow(bytesAcked, bytesLost)

	// Cleanup internal state.
	// This is where we clean up obsolete (acked or lost) packets from the bandwidth sampler.
	// The "least unacked" should actually be first outstanding, but we will only do an estimate
	// using acked / lost packets for now. Because of fast retransmission, they should differ by
	// no more than 2 packets.
	var leastUnacked int64
	if len(ackedPackets) > 0 {
		leastUnacked = ackedPackets[len(ackedPackets)-1].PacketNumber - 2
	} else if len(lostPackets) > 0 {
		leastUnacked = lostPackets[len(lostPackets)-1].PacketNumber + 1
	}
	b.sampler.RemoveObsoletePackets(leastUnacked)
}

// OnApplicationLimited updates BBR sender state when there is no application
// data to send.
func (b *BBRSender) OnApplicationLimited(bytesInFlight int64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if bytesInFlight >= b.getCongestionWindow() {
		return
	}

	b.appLimitedSinceLastProbeRTT = true
	b.sampler.OnAppLimited()
}

// CanSend returns true if a packet can be sent based on the congestion window.
func (b *BBRSender) CanSend(bytesInFlight, bytes int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	pacerCanSend := b.pacer.CanSend(time.Now(), bytes, b.getPacingRate())
	return bytesInFlight < b.getCongestionWindow() && pacerCanSend
}

// BandwidthEstimate returns the estimate of maximum bandwidth.
func (b *BBRSender) BandwidthEstimate() int64 {
	return b.maxBandwidth.GetBest()
}

func (b *BBRSender) getPacingRate() int64 {
	if b.pacingRate <= 0 {
		return int64(highGain * float64(BandwidthFromBytesAndTimeDelta(b.initialCongestionWindow, b.getMinRTT())))
	}
	return b.pacingRate
}

func (b *BBRSender) getCongestionWindow() int64 {
	if b.mode == modeProbeRTT {
		return b.probeRTTCongestionWindow()
	}

	if b.inRecovery() && !b.rateBasedRecovery && !(b.mode == modeStartUp && b.rateBasedStartup) {
		return mathext.Min(b.congestionWindow, b.recoveryWindow)
	}

	return b.congestionWindow
}

func (b *BBRSender) inRecovery() bool {
	return b.recoveryState != stateNotInRecovery
}

func (b *BBRSender) getMinRTT() time.Duration {
	if b.minRTT > 0 {
		return b.minRTT
	}
	return defaultInitialRTT
}

func (b *BBRSender) getTargetCongestionWindow(gain float64) int64 {
	bdp := (b.getMinRTT().Nanoseconds() * b.BandwidthEstimate()) / int64(time.Second)
	congestionWindow := int64(gain * float64(bdp))

	// BDP estimate will be zero if no bandwidth samples are available yet.
	if congestionWindow <= 0 {
		congestionWindow = int64(gain * float64(b.initialCongestionWindow))
	}

	return mathext.Max(congestionWindow, b.minCongestionWindow)
}

func (b *BBRSender) probeRTTCongestionWindow() int64 {
	return b.minCongestionWindow
}

func (b *BBRSender) enterStartupMode() {
	b.mode = modeStartUp
	if log.IsLevelEnabled(log.TraceLevel) {
		log.Tracef("[BBRSender %s] Enter start up mode", b.loggingContext)
	}
	b.pacingGain = highGain
	b.congestionWindowGain = highGain
}

func (b *BBRSender) enterProbeBandwidthMode(now time.Time) {
	b.mode = modeProbeBW
	if log.IsLevelEnabled(log.TraceLevel) {
		log.Tracef("[BBRSender %s] Enter probe bandwidth mode", b.loggingContext)
	}
	b.congestionWindowGain = b.congestionWindowGainConstant

	// Pick a random offset for the gain cycle out of {0, 2..7} range. 1 is
	// excluded because in that case increased gain and decreased gain would not
	// follow each other.
	cycleOffset := mrand.Intn(gainCycleLength - 1)
	if cycleOffset >= 1 {
		cycleOffset++
	}
	b.lastCycleStart = now
	b.pacingGain = pacingGainList[cycleOffset]
}

func (b *BBRSender) discardLostPackets(lostPackets []LostPacketInfo) {
	for _, lost := range lostPackets {
		b.sampler.OnPacketLost(lost.PacketNumber)
	}
}

func (b *BBRSender) updateRoundTripCounter(lastAckedPacket int64) bool {
	if lastAckedPacket > b.currentRoundTripEnd {
		b.roundTripCount++
		b.currentRoundTripEnd = lastAckedPacket
		return true
	}
	return false
}

func (b *BBRSender) updateBandwidthAndMinRTT(now time.Time, ackedPackets []AckedPacketInfo) bool {
	sampleMinRTT := infDuration
	for _, acked := range ackedPackets {
		bandwidthSample := b.sampler.OnPacketAcknowledged(now, acked.PacketNumber)
		if log.IsLevelEnabled(log.TraceLevel) {
			log.Tracef("[BBRSender %s] Acknowledged packet %d => %v", b.loggingContext, acked.PacketNumber, bandwidthSample)
		}
		b.lastSampleIsAppLimited = bandwidthSample.isAppLimited
		if bandwidthSample.rtt > 0 {
			sampleMinRTT = mathext.Min(sampleMinRTT, bandwidthSample.rtt)
		}
		if !bandwidthSample.isAppLimited || bandwidthSample.bandwidth > b.BandwidthEstimate() {
			b.maxBandwidth.Update(bandwidthSample.bandwidth, b.roundTripCount)
		}
	}

	// If none of the RTT samples are valid, return immediately.
	if sampleMinRTT == infDuration {
		return false
	}

	b.minRTTSinceLastProbeRTT = mathext.Min(b.minRTTSinceLastProbeRTT, sampleMinRTT)
	minRTTExpired := b.minRTT > 0 && now.After(b.minRTTTimestamp.Add(minRTTExpiry))
	if b.minRTT <= 0 || minRTTExpired || sampleMinRTT < b.minRTT {
		b.minRTT = sampleMinRTT
		b.minRTTTimestamp = now
		b.minRTTSinceLastProbeRTT = infDuration
		b.appLimitedSinceLastProbeRTT = false
	}
	return minRTTExpired
}

func (b *BBRSender) updateGainCyclePhase(now time.Time, priorInFlight int64, hasLosses bool) {
	// In most cases, the cycle is advanced after an RTT passes.
	shouldAdvanceGainCycling := now.Sub(b.lastCycleStart) > b.getMinRTT()

	// If the pacing gain is above 1.0, the connection is trying to probe the
	// bandwidth by increasing the number of bytes in flight to at least
	// pacing gain * BDP. Make sure that it actually reaches the target, as long
	// as there are no losses suggesting that the buffers are not able to hold
	// that much.
	if b.pacingGain > 1.0 && !hasLosses && priorInFlight < b.getTargetCongestionWindow(b.pacingGain) {
		shouldAdvanceGainCycling = false
	}

	// If pacing gain is below 1.0, the connection is trying to drain the extra
	// queue which could have been incurred by probing prior to it. If the number
	// of bytes in flight falls down to the estimated BDP value earlier, conclude
	// that the queue has been successfully drained and exit this cycle early.
	if b.pacingGain < 1.0 && priorInFlight <= b.getTargetCongestionWindow(1.0) {
		shouldAdvanceGainCycling = true
	}

	if shouldAdvanceGainCycling {
		b.cycleCurrentOffset = (b.cycleCurrentOffset + 1) % gainCycleLength
		b.lastCycleStart = now
		b.pacingGain = pacingGainList[b.cycleCurrentOffset]
	}
}

func (b *BBRSender) checkIfFullBandwidthReached() {
	if b.lastSampleIsAppLimited {
		return
	}

	bandwidthTarget := int64(float64(b.bandwidthAtLastRound) * startUpGrowthTarget)
	if b.BandwidthEstimate() > bandwidthTarget {
		b.bandwidthAtLastRound = b.BandwidthEstimate()
		b.roundsWithoutBandwidthGain = 0
		return
	}

	b.roundsWithoutBandwidthGain++
	if b.roundsWithoutBandwidthGain >= b.numStartupRTTs || (b.exitStartupOnLoss && b.inRecovery()) {
		b.isAtFullBandwidth = true
	}
}

func (b *BBRSender) maybeExitStartupOrDrain(now time.Time) {
	if b.mode == modeStartUp && b.isAtFullBandwidth {
		b.mode = modeDrain
		if log.IsLevelEnabled(log.TraceLevel) {
			log.Tracef("[BBRSender %s] Enter drain mode", b.loggingContext)
		}
		b.pacingGain = drainGain
		b.congestionWindowGain = highGain
	}

	if b.mode == modeDrain && b.bytesInFlight <= b.getTargetCongestionWindow(1) {
		b.enterProbeBandwidthMode(now)
	}
}

func (b *BBRSender) maybeEnterOrExitProbeRTT(now time.Time, isRoundStart bool, minRTTExpired bool) {
	if minRTTExpired && !b.exitingQuiescence && b.mode != modeProbeRTT {
		b.mode = modeProbeRTT
		if log.IsLevelEnabled(log.TraceLevel) {
			log.Tracef("[BBRSender %s] Enter probe RTT mode", b.loggingContext)
		}
		b.pacingGain = 1.0
		// Do not decide on the time to exit PROBE RTT until the bytesInFlight
		// is at the target small value.
		b.exitProbeRTTAt = time.Time{}
	}

	if b.mode == modeProbeRTT {
		b.sampler.OnAppLimited()
		if b.exitProbeRTTAt.IsZero() {
			// If the window has reached the appropriate size, schedule exiting
			// PROBE RTT.
			if b.bytesInFlight < b.probeRTTCongestionWindow()+maxDatagramSize {
				b.exitProbeRTTAt = now.Add(probeRTTTime)
				b.probeRTTRoundPassed = false
			}
		} else {
			if isRoundStart {
				b.probeRTTRoundPassed = true
			}
			if now.After(b.exitProbeRTTAt) && b.probeRTTRoundPassed {
				b.minRTTTimestamp = now
				if !b.isAtFullBandwidth {
					b.enterStartupMode()
				} else {
					b.enterProbeBandwidthMode(now)
				}
			}
		}
	}

	b.exitingQuiescence = false
}

func (b *BBRSender) updateRecoveryState(lastAckedPacket int64, hasLosses bool, isRoundStart bool) {
	// Exit recovery when there are no losses for a round.
	if hasLosses {
		b.endRecoveryAt = b.lastSentPacket
	}

	switch b.recoveryState {
	case stateNotInRecovery:
		// Enter conservation on the first loss.
		if hasLosses {
			b.recoveryState = stateConservation
			if b.mode == modeStartUp {
				b.recoveryState = b.initialConservationInStartup
			}
			// This will cause the recoveryWindow to be set to the correct
			// value in CalculateRecoveryWindow().
			b.recoveryWindow = 0
			// Since the conservation phase is meant to be lasting for a whole
			// round, extend the current round as if it were started right now.
			b.currentRoundTripEnd = b.lastSentPacket
		}
	case stateConservation:
		if isRoundStart {
			b.recoveryState = stateGrowth
		}
		fallthrough
	case stateGrowth:
		// Exit recovery if appropriate.
		if !hasLosses && lastAckedPacket > b.endRecoveryAt {
			b.recoveryState = stateNotInRecovery
		}
	}
}

func (b *BBRSender) updateAckAggregationBytes(ackTime time.Time, newlyAckedBytes int64) {
	// Compute how many bytes are expected to be delivered, assuming max bandwidth
	// is correct.
	expectedBytesAcked := b.maxBandwidth.GetBest() * int64(ackTime.Sub(b.aggregationEpochStartTime)) / int64(time.Second)

	// Reset the current aggregation epoch as soon as the ack arrival rate is less
	// than or equal to the max bandwidth.
	if b.aggregationEpochBytes <= expectedBytesAcked {
		b.aggregationEpochBytes = newlyAckedBytes
		b.aggregationEpochStartTime = ackTime
		return
	}

	// Compute how many extra bytes were delivered vs max bandwidth.
	// Include the bytes most recently acknowledged to account for stretch acks.
	b.aggregationEpochBytes += newlyAckedBytes
	b.maxAckHeight.Update(b.aggregationEpochBytes-expectedBytesAcked, b.roundTripCount)
}

func (b *BBRSender) calculatePacingRate() {
	if b.BandwidthEstimate() <= 0 {
		return
	}

	targetRate := int64(b.pacingGain * float64(b.BandwidthEstimate()))
	if b.rateBasedRecovery && b.inRecovery() {
		b.pacingRate = int64(b.pacingGain * float64(b.maxBandwidth.GetThirdBest()))
	}
	if b.isAtFullBandwidth {
		b.pacingRate = targetRate
		return
	}

	// Pace at the rate of initial window / RTT as soon as RTT measurements are
	// available.
	if b.pacingRate <= 0 && b.rttStats.MinRTT() > 0 {
		b.pacingRate = BandwidthFromBytesAndTimeDelta(b.initialCongestionWindow, b.rttStats.MinRTT())
	}

	// Slow the pacing rate in STARTUP once loss has ever been detected.
	hasEverDetectedLoss := b.endRecoveryAt > 0
	if b.slowerStartup && hasEverDetectedLoss {
		b.pacingRate = int64(StartupAfterLossGain * float64(b.BandwidthEstimate()))
		return
	}

	// Do not decrease the pacing rate during the startup.
	b.pacingRate = mathext.Max(b.pacingRate, targetRate)
}

func (b *BBRSender) calculateCongestionWindow(bytesAcked int64) {
	if b.mode == modeProbeRTT {
		return
	}

	targetWindow := b.getTargetCongestionWindow(b.congestionWindowGain)
	if log.IsLevelEnabled(log.TraceLevel) {
		log.Tracef("[BBRSender %s] targetCongestionWindow=%d", b.loggingContext, targetWindow)
	}

	// Instead of immediately setting the target CWND as the new one, BBR grows
	// the CWND towards targetWindow by only increasing it bytesAcked at a time.
	if b.isAtFullBandwidth {
		b.congestionWindow = mathext.Min(targetWindow, b.congestionWindow+bytesAcked)
	} else if b.congestionWindow < targetWindow || b.sampler.TotalBytesAcked() < b.initialCongestionWindow {
		// If the connection is not yet out of startup phase, do not decrease the
		// window.
		b.congestionWindow += bytesAcked
	}

	// Enforce the limits on the congestion window.
	b.congestionWindow = mathext.Max(b.congestionWindow, b.minCongestionWindow)
	b.congestionWindow = mathext.Min(b.congestionWindow, b.maxCongestionWindow)
}

func (b *BBRSender) calculateRecoveryWindow(bytesAcked int64, bytesLost int64) {
	if b.rateBasedRecovery || (b.mode == modeStartUp && b.rateBasedStartup) {
		return
	}
	if b.recoveryState == stateNotInRecovery {
		return
	}

	if b.recoveryWindow <= 0 {
		// Set up the initial recovery window.
		b.recoveryWindow = b.bytesInFlight + bytesAcked
		b.recoveryWindow = mathext.Max(b.recoveryWindow, b.minCongestionWindow)
		return
	}

	// Remove losses from the recovery window, while accounting for a potential
	// integer underflow.
	if b.recoveryWindow >= bytesLost {
		b.recoveryWindow -= bytesLost
	} else {
		b.recoveryWindow = maxDatagramSize
	}

	// In CONSERVATION mode, just subtracting losses is sufficient. In GROWTH,
	// release additional bytesAcked to achieve a slow-start-like behavior.
	if b.recoveryState == stateGrowth {
		b.recoveryWindow += bytesAcked
	}

	// Sanity checks. Ensure that we always allow to send at least
	// bytesAcked in response.
	b.recoveryWindow = mathext.Max(b.recoveryWindow, b.bytesInFlight+bytesAcked)
	b.recoveryWindow = mathext.Max(b.recoveryWindow, b.minCongestionWindow)
}
