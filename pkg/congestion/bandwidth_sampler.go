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
	"math"
	"sync"
	"time"

	"github.com/enfein/mieru/v3/pkg/mathext"
)

const (
	infiniteBandwidth int64 = math.MaxInt64
)

// BandwidthSample contains a single data point of network bandwidth.
type BandwidthSample struct {
	// The bandwidth in number of bytes at that particular sample.
	// Zero if no valid bandwidth sample is available.
	bandwidth int64

	// The RTT measurement at this particular sample. Zero if no RTT sample is
	// available. Does not correct for delayed ack time.
	rtt time.Duration

	// Indicates whether the sample might be artificially low because the sender
	// did not have enough data to send in order to saturate the link.
	isAppLimited bool
}

func (bs BandwidthSample) String() string {
	return fmt.Sprintf("BandwidthSample{bandwidth=%v, rtt=%v, isAppLimited=%v}", bs.bandwidth, bs.rtt.Truncate(time.Microsecond), bs.isAppLimited)
}

// BandwidthSamplerInterface is an interface common to any class that can
// provide bandwidth samples from the information per individual acknowledged packet.
type BandwidthSamplerInterface interface {
	// OnPacketSent inputs the sent packet information into the sampler. Assumes that
	// all packets are sent in order. The information about the packet will not be
	// released from the sampler until it the packet is either acknowledged or
	// declared lost.
	OnPacketSent(sentTime time.Time, packetNumber, bytes, bytesInFlight int64, hasRetransmittableData bool)

	// OnPacketAcknowledged notifies the sampler that the packetNumber is acknowledged.
	// Returns a bandwidth sample. If no bandwidth sample is available,
	// zero bandwidth is returned.
	OnPacketAcknowledged(ackTime time.Time, packetNumber int64) BandwidthSample

	// OnPacketLost informs the sampler that a packet is considered lost and
	// it should no longer keep track of it.
	OnPacketLost(packetNumber int64)

	// OnAppLimited informs the sampler that the connection is currently
	// app-limited, causing the sampler to enter the app-limited phase.
	// The phase will expire by itself.
	OnAppLimited()

	// RemoveObsoletePackets removes all the packets lower than the specified
	// packet number.
	RemoveObsoletePackets(leastUnacked int64)

	// TotalBytesAcked returns the total number of bytes currently acknowledged
	// by the receiver.
	TotalBytesAcked() int64

	// IsAppLimited returns true if the sampler is in app-limited phase.
	IsAppLimited() bool

	// EndOfAppLimitedPhase returns the last packet number in the
	// app-limited phase.
	EndOfAppLimitedPhase() int64
}

// ConnectionStateOnSentPacket represents the information about a sent packet
// and the state of the connection at the moment the packet was sent,
// specifically the information about the most recently acknowledged packet at
// that moment.
type ConnectionStateOnSentPacket struct {
	// Time at which the packet is sent.
	sentTime time.Time

	// Size of the packet, in number of bytes.
	size int64

	totalBytesSent                      int64 // include the packet itself
	totalBytesSentAtLastAckedPacket     int64
	lastAckedPacketSentTime             time.Time
	lastAckedPacketAckTime              time.Time
	totalBytesAckedAtTheLastAckedPacket int64
	isAppLimited                        bool
}

// NewConnectionStateOnSentPacketFromSampler constructs a new
// ConnectionStateOnSentPacket object from the current state of the bandwidth sampler.
func NewConnectionStateOnSentPacketFromSampler(sendTime time.Time, size int64, sampler *BandwidthSampler) ConnectionStateOnSentPacket {
	return ConnectionStateOnSentPacket{
		sentTime:                            sendTime,
		size:                                size,
		totalBytesSent:                      sampler.totalBytesSent,
		totalBytesSentAtLastAckedPacket:     sampler.totalBytesSentAtLastAckedPacket,
		lastAckedPacketSentTime:             sampler.lastAckedPacketSentTime,
		lastAckedPacketAckTime:              sampler.lastAckedPacketAckTime,
		totalBytesAckedAtTheLastAckedPacket: sampler.totalBytesAcked,
		isAppLimited:                        sampler.isAppLimited,
	}
}

// BandwidthSampler keeps track of sent and acknowledged packets and outputs a
// bandwidth sample for every packet acknowledged. The samples are taken for
// individual packets, and are not filtered; the consumer has to filter the
// bandwidth samples itself. In certain cases, the sampler will locally severely
// underestimate the bandwidth, hence a maximum filter with a size of at least
// one RTT is recommended.
//
// This class bases its samples on the slope of two curves: the number of bytes
// sent over time, and the number of bytes acknowledged as received over time.
// It produces a sample of both slopes for every packet that gets acknowledged,
// based on a slope between two points on each of the corresponding curves. Note
// that due to the packet loss, the number of bytes on each curve might get
// further and further away from each other, meaning that it is not feasible to
// compare byte values coming from different curves with each other.
//
// The obvious points for measuring slope sample are the ones corresponding to
// the packet that was just acknowledged. Let us denote them as S_1 (point at
// which the current packet was sent) and A_1 (point at which the current packet
// was acknowledged). However, taking a slope requires two points on each line,
// so estimating bandwidth requires picking a packet in the past with respect to
// which the slope is measured.
//
// For that purpose, BandwidthSampler always keeps track of the most recently
// acknowledged packet, and records it together with every outgoing packet.
// When a packet gets acknowledged (A_1), it has not only information about when
// it itself was sent (S_1), but also the information about the latest
// acknowledged packet right before it was sent (S_0 and A_0).
//
// Based on that data, send and ack rate are estimated as:
//
//	send_rate = (bytes(S_1) - bytes(S_0)) / (time(S_1) - time(S_0))
//	ack_rate = (bytes(A_1) - bytes(A_0)) / (time(A_1) - time(A_0))
//
// Here, the ack rate is intuitively the rate we want to treat as bandwidth.
// However, in certain cases (e.g. ack compression) the ack rate at a point may
// end up higher than the rate at which the data was originally sent, which is
// not indicative of the real bandwidth. Hence, we use the send rate as an upper
// bound, and the sample value is
//
//	rate_sample = min(send_rate, ack_rate)
//
// An important edge case handled by the sampler is tracking the app-limited
// samples. There are multiple meaning of "app-limited" used interchangeably,
// hence it is important to understand and to be able to distinguish between
// them.
//
// Meaning 1: connection state. The connection is said to be app-limited when
// there is no outstanding data to send. This means that certain bandwidth
// samples in the future would not be an accurate indication of the link
// capacity, and it is important to inform consumer about that. Whenever
// connection becomes app-limited, the sampler is notified via OnAppLimited()
// method.
//
// Meaning 2: a phase in the bandwidth sampler. As soon as the bandwidth
// sampler becomes notified about the connection being app-limited, it enters
// app-limited phase. In that phase, all *sent* packets are marked as
// app-limited. Note that the connection itself does not have to be
// app-limited during the app-limited phase, and in fact it will not be
// (otherwise how would it send packets?). The boolean flag below indicates
// whether the sampler is in that phase.
//
// Meaning 3: a flag on the sent packet and on the sample. If a sent packet is
// sent during the app-limited phase, the resulting sample related to the
// packet will be marked as app-limited.
//
// With the terminology issue out of the way, let us consider the question of
// what kind of situation it addresses.
//
// Consider a scenario where we first send packets 1 to 20 at a regular
// bandwidth, and then immediately run out of data. After a few seconds, we send
// packets 21 to 60, and only receive ack for 21 between sending packets 40 and
// 41. In this case, when we sample bandwidth for packets 21 to 40, the S_0/A_0
// we use to compute the slope is going to be packet 20, a few seconds apart
// from the current packet, hence the resulting estimate would be extremely low
// and not indicative of anything. Only at packet 41 the S_0/A_0 will become 21,
// meaning that the bandwidth sample would exclude the quiescence.
//
// Based on the analysis of that scenario, we implement the following rule: once
// OnAppLimited() is called, all sent packets will produce app-limited samples
// up until an ack for a packet that was sent after OnAppLimited() was called.
// Note that while the scenario above is not the only scenario when the
// connection is app-limited, the approach works in other cases too.
type BandwidthSampler struct {
	mu                              sync.Mutex
	totalBytesSent                  int64
	totalBytesAcked                 int64
	totalBytesSentAtLastAckedPacket int64
	lastAckedPacketSentTime         time.Time
	lastAckedPacketAckTime          time.Time
	lastSentPacket                  int64
	isAppLimited                    bool
	endOfAppLimitedPhase            int64
	connectionStateMap              *PacketNumberIndexedQueue[ConnectionStateOnSentPacket]
}

var _ BandwidthSamplerInterface = &BandwidthSampler{}

func NewBandwidthSampler() BandwidthSamplerInterface {
	return &BandwidthSampler{
		connectionStateMap: NewPacketNumberIndexedQueue[ConnectionStateOnSentPacket](),
	}
}

func (bs *BandwidthSampler) OnPacketSent(sentTime time.Time, packetNumber int64, bytes int64, bytesInFlight int64, hasRetransmittableData bool) {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	bs.lastSentPacket = packetNumber
	if !hasRetransmittableData {
		return
	}
	bs.totalBytesSent += bytes

	// If there are no packets in flight, the time at which the new transmission
	// opens can be treated as the A_0 point for the purpose of bandwidth
	// sampling. This underestimates bandwidth to some extent, and produces some
	// artificially low samples for most packets in flight, but it provides with
	// samples at important points where we would not have them otherwise, most
	// importantly at the beginning of the connection.
	if bytesInFlight == 0 {
		bs.lastAckedPacketAckTime = sentTime
		bs.totalBytesSentAtLastAckedPacket = bs.totalBytesSent

		// In this situation ack compression is not a concern, set send rate to
		// effectively infinite.
		bs.lastAckedPacketSentTime = sentTime
	}

	bs.connectionStateMap.Emplace(packetNumber, NewConnectionStateOnSentPacketFromSampler(sentTime, bytes, bs))
}

func (bs *BandwidthSampler) OnPacketAcknowledged(ackTime time.Time, packetNumber int64) BandwidthSample {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	sentPacket := bs.connectionStateMap.GetEntry(packetNumber)
	if sentPacket == nil {
		return BandwidthSample{}
	}

	sample := bs.onPacketAcknowledgedInner(ackTime, packetNumber, sentPacket)
	bs.connectionStateMap.Remove(packetNumber)
	return sample
}

func (bs *BandwidthSampler) OnPacketLost(packetNumber int64) {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	bs.connectionStateMap.Remove(packetNumber)
}

func (bs *BandwidthSampler) OnAppLimited() {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	bs.isAppLimited = true
	bs.endOfAppLimitedPhase = bs.lastSentPacket
}

func (bs *BandwidthSampler) RemoveObsoletePackets(leastUnacked int64) {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	for !bs.connectionStateMap.IsEmpty() && bs.connectionStateMap.FirstPacket() < leastUnacked {
		bs.connectionStateMap.Remove(bs.connectionStateMap.FirstPacket())
	}
}

func (bs *BandwidthSampler) TotalBytesAcked() int64 {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	return bs.totalBytesAcked
}

func (bs *BandwidthSampler) IsAppLimited() bool {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	return bs.isAppLimited
}

func (bs *BandwidthSampler) EndOfAppLimitedPhase() int64 {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	return bs.endOfAppLimitedPhase
}

func (bs *BandwidthSampler) onPacketAcknowledgedInner(ackTime time.Time, packetNumber int64, sentPacket *ConnectionStateOnSentPacket) BandwidthSample {
	bs.totalBytesAcked += sentPacket.size
	bs.totalBytesSentAtLastAckedPacket = sentPacket.totalBytesSent
	bs.lastAckedPacketSentTime = sentPacket.sentTime
	bs.lastAckedPacketAckTime = ackTime

	// Exit app-limited phase once a packet that was sent while the connection is
	// not app-limited is acknowledged.
	if bs.isAppLimited && packetNumber > bs.endOfAppLimitedPhase {
		bs.isAppLimited = false
	}

	// There might have been no packets acknowledged at the moment when the
	// current packet was sent. In that case, there is no bandwidth sample to
	// make.
	if sentPacket.lastAckedPacketSentTime.IsZero() {
		return BandwidthSample{}
	}

	// Infinite rate indicates that the sampler is supposed to discard the
	// current send rate sample and use only the ack rate.
	sendRate := infiniteBandwidth
	if sentPacket.sentTime.After(sentPacket.lastAckedPacketSentTime) {
		sendRate = BandwidthFromBytesAndTimeDelta(sentPacket.totalBytesSent-sentPacket.totalBytesSentAtLastAckedPacket, sentPacket.sentTime.Sub(sentPacket.lastAckedPacketSentTime))
	}

	// During the slope calculation, ensure that ack time of the current packet is
	// always larger than the time of the previous packet, otherwise division by
	// zero or integer underflow can occur.
	if !ackTime.After(sentPacket.lastAckedPacketAckTime) {
		return BandwidthSample{}
	}
	ackRate := BandwidthFromBytesAndTimeDelta(bs.totalBytesAcked-sentPacket.totalBytesAckedAtTheLastAckedPacket, ackTime.Sub(sentPacket.lastAckedPacketAckTime))

	sample := BandwidthSample{
		bandwidth:    mathext.Min(sendRate, ackRate),
		rtt:          ackTime.Sub(sentPacket.sentTime),
		isAppLimited: sentPacket.isAppLimited,
	}
	return sample
}

func BandwidthFromBytesAndTimeDelta(bytes int64, duration time.Duration) int64 {
	if duration <= 0 {
		return 0
	}
	return bytes * int64(time.Second) / int64(duration)
}
