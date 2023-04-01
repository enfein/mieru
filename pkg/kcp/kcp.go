package kcp

import (
	crand "crypto/rand"
	"fmt"
	"math"
	"time"

	"github.com/enfein/mieru/pkg/cipher"
	"github.com/enfein/mieru/pkg/congestion"
	"github.com/enfein/mieru/pkg/log"
	"github.com/enfein/mieru/pkg/mathext"
	"github.com/enfein/mieru/pkg/metrics"
	"github.com/enfein/mieru/pkg/rng"
	"github.com/enfein/mieru/pkg/slicepool"
	"github.com/enfein/mieru/pkg/stderror"
)

const (

	// Overall outer header size needed by data encryption.
	OuterHeaderSize = cipher.DefaultOverhead + cipher.DefaultNonceSize

	// Maximum packet buffer size.
	MaxBufSize = 1500

	// Maximum MTU of UDP packet. UDP overhead is 8 bytes, IP overhead is maximum 40 bytes.
	MaxMTU = MaxBufSize - 48

	IKCP_CMD_VER     = 0                   // version of command set
	IKCP_CMD_MAX_VER = 7                   // maximum version of command set
	IKCP_CMD_MAX_NUM = 15                  // maximum command number
	IKCP_CMD_PUSH    = 1 + IKCP_CMD_VER<<5 // cmd: send data
	IKCP_CMD_ACK     = 2 + IKCP_CMD_VER<<5 // cmd: acknowledge of a received packet
	IKCP_CMD_WASK    = 3 + IKCP_CMD_VER<<5 // cmd: ask remote window size
	IKCP_CMD_WINS    = 4 + IKCP_CMD_VER<<5 // cmd: reply my window size

	IKCP_ASK_SEND = 1 // need to send IKCP_CMD_WASK
	IKCP_ASK_TELL = 2 // need to send IKCP_CMD_WINS

	IKCP_WND_SND_MIN = 32    // minimum send window size (number of packets)
	IKCP_WND_SND_MAX = 65535 // maximum send window size (number of packets)
	IKCP_WND_RCV     = 65535 // maximum receive window size (number of packets)

	IKCP_MTU_DEF = MaxMTU - OuterHeaderSize // KCP MTU

	IKCP_ACK_FAST       = 3    // do retransmission after receiving the number of out of order ACK
	IKCP_INTERVAL       = 10   // event loop interval in milliseconds
	IKCP_PROBE_INTERVAL = 1000 // window probe interval in milliseconds
	IKCP_DEADLINK       = 30   // number of retransmissions before link is dead

	IKCP_OVERHEAD         = 24 // size of KCP header
	IKCP_SN_OFFSET        = 12 // offset to get sequence number in KCP header
	IKCP_TOTAL_LEN_OFFSET = 22 // offset to get segment total length (data + padding)
)

var (
	// PktCachePool is a system-wide packet buffer shared among sending, receiving to mitigate
	// high-frequency memory allocation for packets.
	PktCachePool slicepool.SlicePool

	// refTime is a monotonic reference time point.
	refTime time.Time = time.Now()

	// maxPaddingSize is the maximum size of padding added to a single KCP segment.
	maxPaddingSize = 256 + rng.FixedInt(256)
)

var (
	// Number of KCP bytes sent for upper level.
	BytesSent = metrics.RegisterMetric("KCP", "BytesSent")

	// Number of KCP bytes received from lower level.
	BytesReceived = metrics.RegisterMetric("KCP", "BytesReceived")

	// Number of incoming segments.
	InSegs = metrics.RegisterMetric("KCP", "InSegs")

	// Number of outgoing segments.
	OutSegs = metrics.RegisterMetric("KCP", "OutSegs")

	// Number of repeated segments.
	RepeatSegs = metrics.RegisterMetric("KCP", "RepeatSegs")

	// Number of lost segments.
	LostSegs = metrics.RegisterMetric("KCP", "LostSegs")

	// Number of out of window segments.
	OutOfWindowSegs = metrics.RegisterMetric("KCP", "OutOfWindowSegs")

	// Number of fast retransmission segments.
	FastRetransSegs = metrics.RegisterMetric("KCP", "FastRetransSegs")

	// Number of total retransmission segments.
	RetransSegs = metrics.RegisterMetric("KCP", "RetransSegs")
)

func init() {
	PktCachePool = slicepool.NewSlicePool(MaxBufSize)
}

// currentMs returns current elapsed monotonic milliseconds since program startup.
func currentMs() uint32 { return uint32(time.Since(refTime) / time.Millisecond) }

// outputCallback is a prototype which ought capture conn and call conn.Write.
type outputCallback func(buf []byte, size int)

// segment defines a KCP segment.
type segment struct {
	conv uint32 // byte 0 - 3: conversation ID
	cmd  uint8  // byte 4: command
	frg  uint8  // byte 5: fragment count
	wnd  uint16 // byte 6 - 7: my receive window size
	ts   uint32 // byte 8 - 11: timestamp
	sn   uint32 // byte 12 - 15: sequence number
	una  uint32 // byte 16 - 19: un-acknowledged sequence number (what do I want to receive next)
	// byte 20 - 21: data length
	// byte 22 - 23: data + padding length

	rto      time.Duration // retransmission timeout
	xmit     uint32        // times of (re)transmission
	resendTs uint32        // when should we resend the packet
	fastAck  uint32        // number of out of order ACK received
	acked    bool          // mark if the seg has acked
	data     []byte        // actual data
}

// String returns a string representation of the segment.
func (seg *segment) String() string {
	return fmt.Sprintf("{conv=%d, cmd=%s, frg=%d, wnd=%d, ts=%d, sn=%d, una=%d, len=%d}",
		seg.conv, Command2Str(int(seg.cmd)), seg.frg, seg.wnd, seg.ts, seg.sn, seg.una, len(seg.data))
}

// encode encodes a KCP segment header into the given buffer.
func (seg *segment) encode(ptr []byte) []byte {
	ptr = encode32u(ptr, seg.conv)
	ptr = encode8u(ptr, seg.cmd)
	ptr = encode8u(ptr, seg.frg)
	ptr = encode16u(ptr, seg.wnd)
	ptr = encode32u(ptr, seg.ts)
	ptr = encode32u(ptr, seg.sn)
	ptr = encode32u(ptr, seg.una)
	ptr = encode16u(ptr, uint16(len(seg.data)))
	ptr = encode16u(ptr, uint16(len(seg.data))) // It will be adjusted when padding is added.
	OutSegs.Add(1)
	return ptr
}

// Command2Str returns the display name of the KCP command.
func Command2Str(cmd int) string {
	cmdStr := "UNKNOWN"
	switch cmd {
	case IKCP_CMD_PUSH:
		cmdStr = "PUSH"
	case IKCP_CMD_ACK:
		cmdStr = "ACK"
	case IKCP_CMD_WASK:
		cmdStr = "WINDOW_ASK"
	case IKCP_CMD_WINS:
		cmdStr = "WINDOW_SIZE"
	default:
	}
	return cmdStr
}

// KCP defines a single KCP connection.
type KCP struct {
	// Basic information.
	conv         uint32 // conversation ID (must match between two endpoints)
	mtu          uint32 // KCP maximum transmission unit
	mss          uint32 // KCP maximum segment size
	streamMode   bool   // streaming mode or packet mode
	disconnected bool   // if connection is down

	// Flow control.
	sendUna       uint32                         // the sequence number of next packet that we have sent but not acknowledged by remote
	sendNext      uint32                         // the sequence number of next packet to send
	recvNext      uint32                         // the sequence number of next packet expected to be received by application
	recvWindow    uint16                         // our receiving window size
	remoteWindow  uint16                         // remote receiving window size
	fastResend    uint32                         // the number of out of order ACK to trigger fast retransmission, use 0 to disable this feature
	rttStat       *congestion.RTTStats           // round trip time statistics
	sendAlgorithm *congestion.CubicSendAlgorithm // congestion control algorithm

	// Operation control.
	interval uint32 // output interval
	probe    uint32 // whether needs to send window probe
	tsProbe  uint32 // time to send probe
	deadLink uint32 // number of retry before mark the link as disconnected

	lastInputTime  time.Time // last timestamp when Input() is called
	lastOutputTime time.Time // last timestamp when Output() is called

	sendQueue []segment // segments that waiting to be sent over the network
	recvQueue []segment // segments that waiting to be read by application
	sendBuf   []segment // segments that are sent but not acknowledged by remote
	recvBuf   []segment // segments that are received from the network

	ackList []ackItem // the ACK that needs to be sent later

	buffer     []byte
	reserved   int // number of reserved bytes at the beginning of buffer
	outputCall outputCallback
}

type ackItem struct {
	sn uint32 // sequence number
	ts uint32 // timestamp
}

// NewKCP create a new kcp state machine.
//
// 'conv' must be equal in the connection peers, or else data will be silently rejected.
//
// 'output' function will be called whenever these is data to be sent on wire.
func NewKCP(conv uint32, output outputCallback) *KCP {
	kcp := new(KCP)
	kcp.conv = conv
	kcp.recvWindow = IKCP_WND_RCV
	kcp.remoteWindow = IKCP_WND_RCV
	kcp.mtu = IKCP_MTU_DEF
	kcp.mss = kcp.mtu - IKCP_OVERHEAD
	kcp.buffer = make([]byte, kcp.mtu)
	kcp.fastResend = IKCP_ACK_FAST
	kcp.rttStat = congestion.NewRTTStats()
	kcp.rttStat.SetMaxAckDelay(2 * IKCP_INTERVAL * time.Millisecond)
	kcp.rttStat.SetRTOMultiplier(1.5)
	kcp.sendAlgorithm = congestion.NewCubicSendAlgorithm(IKCP_WND_SND_MIN, IKCP_WND_SND_MAX)
	kcp.interval = IKCP_INTERVAL
	kcp.deadLink = IKCP_DEADLINK
	kcp.outputCall = output
	return kcp
}

// -------- public KCP methods --------

// ReserveBytes keeps n bytes untouched from the beginning of the buffer,
// the outputCallback function should be aware of this.
//
// Return false if n >= mss
func (kcp *KCP) ReserveBytes(n int) bool {
	if n >= int(kcp.mtu-IKCP_OVERHEAD) || n < 0 {
		return false
	}
	kcp.reserved = n
	kcp.mss = kcp.mtu - IKCP_OVERHEAD - uint32(n)
	return true
}

// SetPollIntervalMs set KCP polling interval in milliseconds.
func (kcp *KCP) SetPollIntervalMs(interval uint32) {
	if interval == 0 {
		panic("KCP poll interval can't be 0")
	}
	kcp.interval = interval
}

// Input a packet into kcp state machine, by underlay protocol.
//
// 'ackNoDelay' will trigger immediate ACK, but surely it will not be efficient in bandwidth.
func (kcp *KCP) Input(data []byte, ackNoDelay bool) error {
	if len(data) < IKCP_OVERHEAD {
		log.Warnf("data size %d is smaller than KCP header size %d", len(data), IKCP_OVERHEAD)
		return stderror.ErrNoEnoughData
	}

	var ourReceiveWindowSizeChanged bool
	var hasAck bool
	var ackLatestTimestamp uint32
	var inSegs int64

	// There can be multiple KCP segments in a single UDP packet.
	for {
		var ts, sn, una, conv uint32
		var wnd, dlen, tlen uint16
		var cmd, frg uint8

		// Exit the loop when there is no enough data to read.
		if len(data) < int(IKCP_OVERHEAD) {
			break
		}

		// Read the KCP header from data.
		data = decode32u(data, &conv)
		if conv != kcp.conv {
			log.Warnf("expect KCP conversation ID %d, got %d", kcp.conv, conv)
			return stderror.ErrIDNotMatch
		}
		data = decode8u(data, &cmd)
		data = decode8u(data, &frg)
		data = decode16u(data, &wnd)
		data = decode32u(data, &ts)
		data = decode32u(data, &sn)
		data = decode32u(data, &una)
		data = decode16u(data, &dlen)
		data = decode16u(data, &tlen)

		// The remaining data length should be >= total length of KCP segment.
		if len(data) < int(tlen) {
			log.Warnf("data size %d is smaller than KCP length %d", len(data), tlen)
			return stderror.ErrOutOfRange
		}

		if cmd != IKCP_CMD_PUSH && cmd != IKCP_CMD_ACK &&
			cmd != IKCP_CMD_WASK && cmd != IKCP_CMD_WINS {
			log.Warnf("KCP command %s can't be recognized", Command2Str(int(cmd)))
			return stderror.ErrUnknownCommand
		}

		kcp.lastInputTime = time.Now()

		// The remote receiving window size is sent with each KCP segment.
		kcp.remoteWindow = wnd

		// The unacknowledged sequence number is also sent with each KCP segment.
		if kcp.removeAckedPktsFromSendBuf(una) > 0 {
			// Some segments have been removed from send buf, our receiving window size is changed.
			ourReceiveWindowSizeChanged = true
		}
		kcp.adjustSendUna()

		if cmd == IKCP_CMD_ACK {
			hasAck = true
			ackLatestTimestamp = ts
			kcp.processAck(sn)
			kcp.processFastAck(sn, ts)
		} else if cmd == IKCP_CMD_PUSH {
			if len(kcp.recvBuf) >= IKCP_WND_RCV {
				kcp.recvWindow = 0
			} else {
				kcp.recvWindow = uint16(IKCP_WND_RCV - len(kcp.recvBuf))
			}
			if timediff(sn, kcp.recvNext+uint32(kcp.recvWindow)) < 0 {
				// Append ack even for already received packets.
				kcp.appendToAckList(sn, ts)
				if timediff(sn, kcp.recvNext) >= 0 {
					var seg segment
					seg.conv = conv
					seg.cmd = cmd
					seg.frg = frg
					seg.wnd = wnd
					seg.ts = ts
					seg.sn = sn
					seg.una = una
					seg.data = data[:dlen]                 // remove the padding
					repeat := kcp.processReceivedData(seg) // if the segment is repeated
					if repeat {
						RepeatSegs.Add(1)
					}
				} else {
					OutOfWindowSegs.Add(1)
				}
			} else {
				OutOfWindowSegs.Add(1)
			}
		} else if cmd == IKCP_CMD_WASK {
			kcp.probe |= IKCP_ASK_TELL
		} else if cmd == IKCP_CMD_WINS {
			// Nothing to do here. The remote window size is automatically updated by processing each segment.
		} else {
			return stderror.ErrUnknownCommand
		}

		inSegs++

		// Now read the next KCP segment.
		data = data[tlen:]
	}
	InSegs.Add(inSegs)

	// Update RTT with the latest timestamp in ACK packet.
	if hasAck {
		current := currentMs()
		if timediff(current, ackLatestTimestamp) >= 0 {
			kcp.rttStat.UpdateRTT(time.Duration(timediff(current, ackLatestTimestamp)) * time.Millisecond)
		}
	}

	if ourReceiveWindowSizeChanged {
		// Send out data along with our window size.
		kcp.Output(false)
	} else if ackNoDelay && len(kcp.ackList) > 0 {
		// Send ACK only.
		kcp.Output(true)
	}
	return nil
}

// Upper layer sends data to kcp state machine.
func (kcp *KCP) Send(buffer []byte) error {
	if kcp.disconnected {
		return stderror.ErrDisconnected
	}
	if len(buffer) == 0 {
		log.Warnf("data to send is empty")
		return stderror.ErrInvalidArgument
	}
	if len(buffer) > 65535 {
		log.Warnf("data to send is too big, maximum 65535 bytes")
		return stderror.ErrOutOfRange
	}

	// In streaming mode, append message to previous segment if possible.
	if kcp.streamMode {
		n := len(kcp.sendQueue)
		if n > 0 {
			seg := &kcp.sendQueue[n-1]
			if len(seg.data) < int(kcp.mss) {
				remaining := int(kcp.mss) - len(seg.data)
				extend := mathext.Min(remaining, len(buffer))
				oldLen := len(seg.data)
				seg.data = seg.data[:oldLen+extend]
				copy(seg.data[oldLen:], buffer)
				buffer = buffer[extend:]
			}
		}
		// Return if all data have been appended to the last segment in send queue.
		if len(buffer) == 0 {
			return nil
		}
	}

	// Calculate number of fragments needed to send the data.
	var nfrg int
	if len(buffer) <= int(kcp.mss) {
		nfrg = 1
	} else {
		nfrg = (len(buffer) + int(kcp.mss) - 1) / int(kcp.mss)
	}
	if nfrg > 255 {
		log.Warnf("data to send is too big, can't fit into 255 segments")
		return stderror.ErrOutOfRange
	}
	if nfrg == 0 {
		nfrg = 1
	}

	// Create segments and append to send queue.
	for i := 0; i < nfrg; i++ {
		size := mathext.Min(len(buffer), int(kcp.mss))
		seg := kcp.newSegment(size)
		copy(seg.data, buffer[:size])
		if kcp.streamMode {
			// Fragment number is always 0 in streaming mode.
			seg.frg = 0
		} else {
			seg.frg = uint8(nfrg - i - 1)
		}
		kcp.sendQueue = append(kcp.sendQueue, seg)
		buffer = buffer[size:]
	}
	return nil
}

// Send a heartbeat packet to remote to update remote's lastInputTime.
// This is implemented by asking KCP to probe the remote window size.
func (kcp *KCP) SendHeartbeat() {
	kcp.probe |= IKCP_ASK_SEND
}

// Upper layer receives data from kcp state machine.
// The received data is copied into the buffer provided by caller.
// The buffer must be big enough to hold input KCP message (which may have multiple fragments).
//
// Return number of bytes read, or -1 on error.
func (kcp *KCP) Recv(buffer []byte) (n int, err error) {
	peeksize := kcp.PeekSize()
	if peeksize < 0 {
		return -1, stderror.ErrNoEnoughData
	}
	if peeksize > len(buffer) {
		return -1, stderror.ErrOutOfRange
	}

	var fastRecover bool
	if len(kcp.recvQueue) >= int(kcp.recvWindow) {
		// Our receive queue is too long. Advertise our window size to the sender.
		fastRecover = true
	}

	// If there are multiple fragments, merge them in receive queue.
	// Otherwise, take the first segment from the receive queue.
	rmCount := 0 // number of packets to remove from receive queue.
	for k := range kcp.recvQueue {
		seg := &kcp.recvQueue[k]
		copy(buffer, seg.data)
		buffer = buffer[len(seg.data):]
		n += len(seg.data)
		rmCount++
		kcp.delSegment(seg)
		if seg.frg == 0 {
			break
		}
	}

	if rmCount > 0 {
		kcp.recvQueue = kcp.removeFront(kcp.recvQueue, rmCount)
	}

	// Move available data from receive buf to receive queue.
	mvCount := 0
	for k := range kcp.recvBuf {
		seg := &kcp.recvBuf[k]
		if seg.sn == kcp.recvNext {
			kcp.recvNext++
			mvCount++
		} else {
			break
		}
	}
	if mvCount > 0 {
		kcp.recvQueue = append(kcp.recvQueue, kcp.recvBuf[:mvCount]...)
		kcp.recvBuf = kcp.removeFront(kcp.recvBuf, mvCount)
	}

	if len(kcp.recvQueue) < int(kcp.recvWindow) && fastRecover {
		// Our receive queue now have empty spaces.
		// Ready to send back IKCP_CMD_WINS in ikcp_flush
		// tell remote my window size.
		kcp.probe |= IKCP_ASK_TELL
	}
	return
}

// Output sends our accumulated data to the remote.
// Returns the time duration (in milliseconds) that next `Output` should be called.
func (kcp *KCP) Output(ackOnly bool) uint32 {
	var seg segment
	seg.conv = kcp.conv
	seg.cmd = IKCP_CMD_ACK
	seg.wnd = kcp.recvWindow
	seg.sn = kcp.sendNext
	seg.una = kcp.recvNext

	buffer := kcp.buffer
	ptr := buffer[kcp.reserved:]
	lastSegIdx := kcp.reserved - IKCP_OVERHEAD // the starting index of the last segment in buffer
	prevSegDataLen := 0                        // the additional segment index offset introduced by payload in the segment

	// addPadding rewrites the last segment and introduces a padding. It returns the padding size.
	addPadding := func() int {
		paddingSize := 0
		// Only add padding when there is at least one segment in the buffer.
		if lastSegIdx >= kcp.reserved {
			remainingSpace := len(ptr)
			if remainingSpace > 0 {
				lastSegPtr := buffer[lastSegIdx:]
				var segTotalLen uint16
				decode16u(lastSegPtr[IKCP_TOTAL_LEN_OFFSET:], &segTotalLen)
				paddingSize = rng.Intn(mathext.Min(maxPaddingSize, remainingSpace))
				if paddingSize > 0 {
					crand.Read(lastSegPtr[IKCP_OVERHEAD+int(segTotalLen) : IKCP_OVERHEAD+int(segTotalLen)+paddingSize])
					segTotalLen += uint16(paddingSize)
					encode16u(lastSegPtr[IKCP_TOTAL_LEN_OFFSET:], segTotalLen)
					ptr = ptr[paddingSize:]
					metrics.OutPaddingBytes.Add(int64(paddingSize))
				}
			}
		}
		return paddingSize
	}

	// reserve sends out the current data in the buffer
	// if it can't make the room for the newly added space.
	reserve := func(space int) {
		usedSize := len(buffer) - len(ptr)
		if usedSize+space > int(kcp.mtu) {
			paddingSize := addPadding()
			kcp.outputCall(buffer, usedSize+paddingSize)
			ptr = buffer[kcp.reserved:]
			lastSegIdx = kcp.reserved - IKCP_OVERHEAD
			prevSegDataLen = 0
			kcp.lastOutputTime = time.Now()
		}
	}

	// flushBuffer sends out all the remaining data in the buffer.
	// It returns true if any data is sent.
	flushBuffer := func() bool {
		usedSize := len(buffer) - len(ptr)
		if usedSize > kcp.reserved {
			paddingSize := addPadding()
			kcp.outputCall(buffer, usedSize+paddingSize)
			kcp.lastOutputTime = time.Now()
			return true
		}
		return false
	}

	// Process pending acknowledges. For each segment that can be acknowledged,
	// create an ACK and append to the buffer.
	for _, ack := range kcp.ackList {
		reserve(IKCP_OVERHEAD)
		seg.sn, seg.ts = ack.sn, ack.ts
		ptr = seg.encode(ptr)
		lastSegIdx += IKCP_OVERHEAD
	}
	// Clear pending acknowledges.
	kcp.ackList = kcp.ackList[:0]

	if ackOnly {
		flushBuffer()
		return kcp.interval
	}

	// Probe remote window size if it is zero.
	if kcp.remoteWindow == 0 {
		current := currentMs()
		if kcp.tsProbe == 0 {
			kcp.tsProbe = current
			kcp.probe |= IKCP_ASK_SEND
		} else {
			// Last probe is not successful.
			if timediff(current, kcp.tsProbe) > IKCP_PROBE_INTERVAL {
				kcp.tsProbe = current
				kcp.probe |= IKCP_ASK_SEND
			}
		}
	} else {
		kcp.tsProbe = 0
	}

	if (kcp.probe & IKCP_ASK_SEND) != 0 {
		// Append the window probe request into the segment.
		seg.cmd = IKCP_CMD_WASK
		reserve(IKCP_OVERHEAD)
		ptr = seg.encode(ptr)
		lastSegIdx += IKCP_OVERHEAD
	}

	if (kcp.probe & IKCP_ASK_TELL) != 0 {
		// Append the window probe response into the segment.
		seg.cmd = IKCP_CMD_WINS
		reserve(IKCP_OVERHEAD)
		ptr = seg.encode(ptr)
		lastSegIdx += IKCP_OVERHEAD
	}

	kcp.probe = 0

	// The initial congestion window size is set to the smaller of
	// our send window size and remote receive window size.
	cwnd := mathext.Min(kcp.sendAlgorithm.CongestionWindowSize(), uint32(kcp.remoteWindow))

	// Prepare sending data by moving data from send queue to send buf,
	// up to send una + cwnd.
	newSegsCount := 0
	for k := range kcp.sendQueue {
		if timediff(kcp.sendNext, kcp.sendUna+cwnd) >= 0 {
			break
		}
		newseg := kcp.sendQueue[k]
		newseg.conv = kcp.conv
		newseg.cmd = IKCP_CMD_PUSH
		newseg.sn = kcp.sendNext
		kcp.sendBuf = append(kcp.sendBuf, newseg)
		kcp.sendNext++
		newSegsCount++
	}
	if newSegsCount > 0 {
		kcp.sendQueue = kcp.removeFront(kcp.sendQueue, newSegsCount)
	}

	// If fastResend is set to 0, the fast resend on out of order ACK is disabled.
	fastResend := kcp.fastResend
	if kcp.fastResend == 0 {
		fastResend = 0xffffffff
	}

	// check for retransmissions
	current := currentMs()
	var fastRetransSegs, lostSegs int64

	for k := range kcp.sendBuf {
		segment := &kcp.sendBuf[k]
		needsend := false
		if segment.acked {
			continue
		}
		if segment.xmit == 0 {
			// Initial transmit.
			needsend = true
			segment.rto = kcp.rttStat.RTO()
			segment.resendTs = current + uint32(segment.rto/time.Millisecond)
		} else if segment.fastAck >= fastResend {
			// Fast retransmit after received multiple acks with old sequence numbers.
			needsend = true
			segment.fastAck = 0
			segment.rto = kcp.rttStat.RTO()
			segment.resendTs = current + uint32(segment.rto/time.Millisecond)
			fastRetransSegs++
		} else if timediff(current, segment.resendTs) >= 0 {
			// Retransmit timeout segment.
			needsend = true
			segment.rto += kcp.rttStat.RTO()
			segment.fastAck = 0
			segment.resendTs = current + uint32(segment.rto/time.Millisecond)
			lostSegs++
		}

		if needsend {
			current = currentMs()
			segment.xmit++
			segment.ts = current
			segment.wnd = seg.wnd
			segment.una = seg.una

			need := IKCP_OVERHEAD + len(segment.data)
			reserve(need)

			ptr = segment.encode(ptr)
			copy(ptr, segment.data)
			ptr = ptr[len(segment.data):]

			lastSegIdx += IKCP_OVERHEAD + prevSegDataLen
			prevSegDataLen = len(segment.data)

			if segment.xmit >= kcp.deadLink {
				kcp.disconnected = true
			}
		}
	}

	// Flash remaining segments.
	flushBuffer()

	// Update counters.
	var totalRetransSegs int64
	if lostSegs > 0 {
		LostSegs.Add(lostSegs)
		totalRetransSegs += lostSegs
	}
	if fastRetransSegs > 0 {
		FastRetransSegs.Add(fastRetransSegs)
		totalRetransSegs += fastRetransSegs
	}
	if totalRetransSegs > 0 {
		RetransSegs.Add(totalRetransSegs)
	}

	// Update congestion window.
	if totalRetransSegs > 0 {
		kcp.sendAlgorithm.OnLoss()
	}

	return kcp.interval
}

// PeekSize checks the size of next message in the recv queue.
// It includes all the fragments of the message.
// Return 0 if the message has length 0 but recv queue is not empty.
// Return -1 if recv queue is empty.
func (kcp *KCP) PeekSize() (length int) {
	if len(kcp.recvQueue) == 0 {
		return -1
	}

	seg := &kcp.recvQueue[0]
	if seg.frg == 0 {
		return len(seg.data)
	}

	// Return error if some fragments are unavailable.
	if len(kcp.recvQueue) < int(seg.frg+1) {
		return -1
	}

	for k := range kcp.recvQueue {
		seg := &kcp.recvQueue[k]
		length += len(seg.data)
		if seg.frg == 0 {
			break
		}
	}
	return
}

func (kcp *KCP) LastInputTime() time.Time {
	return kcp.lastInputTime
}

func (kcp *KCP) LastOutputTime() time.Time {
	return kcp.lastOutputTime
}

// SetMtu changes MTU size.
func (kcp *KCP) SetMtu(mtu int) error {
	if mtu < IKCP_OVERHEAD {
		log.Errorf("MTU is smaller than KCP overhead %d", IKCP_OVERHEAD)
		return stderror.ErrInvalidArgument
	}
	if kcp.reserved >= int(kcp.mtu-IKCP_OVERHEAD) || kcp.reserved < 0 {
		log.Errorf("no enough space for reserved bytes after set MTU")
		return stderror.ErrInvalidArgument
	}

	buffer := make([]byte, mtu)
	kcp.mtu = uint32(mtu)
	kcp.mss = kcp.mtu - IKCP_OVERHEAD - uint32(kcp.reserved)
	kcp.buffer = buffer
	return nil
}

// WaitSendSize gets how many packet is waiting to be sent.
func (kcp *KCP) WaitSendSize() int {
	return len(kcp.sendBuf) + len(kcp.sendQueue)
}

// ReleaseTX releases all cached outgoing segments.
func (kcp *KCP) ReleaseTX() {
	for k := range kcp.sendQueue {
		if kcp.sendQueue[k].data != nil {
			PktCachePool.Put(kcp.sendQueue[k].data)
		}
	}
	for k := range kcp.sendBuf {
		if kcp.sendBuf[k].data != nil {
			PktCachePool.Put(kcp.sendBuf[k].data)
		}
	}
	kcp.sendQueue = kcp.sendQueue[:0]
	kcp.sendBuf = kcp.sendBuf[:0]
}

func (kcp *KCP) ConversationID() uint32 {
	return kcp.conv
}

func (kcp *KCP) MSS() uint32 {
	return kcp.mss
}

func (kcp *KCP) SendWindow() uint32 {
	return kcp.sendAlgorithm.CongestionWindowSize()
}

func (kcp *KCP) RecvWindow() uint32 {
	return uint32(kcp.recvWindow)
}

func (kcp *KCP) RemoteWindow() uint32 {
	return uint32(kcp.remoteWindow)
}

func (kcp *KCP) StreamMode() bool {
	return kcp.streamMode
}

func (kcp *KCP) SetStreamMode(mode bool) {
	kcp.streamMode = mode
}

// -------- private KCP methods --------

// newSegment creates a KCP segment.
func (kcp *KCP) newSegment(size int) (seg segment) {
	seg.data = PktCachePool.Get()[:size]
	return
}

// delSegment recycles a KCP segment.
func (kcp *KCP) delSegment(seg *segment) {
	if seg.data != nil {
		PktCachePool.Put(seg.data)
		seg.data = nil
	}
}

// adjustSendUna adjusts our send unacknowledged sequence number based on the next packet
// we expected to receive ACK in the send buf.
func (kcp *KCP) adjustSendUna() {
	if len(kcp.sendBuf) > 0 {
		seg := &kcp.sendBuf[0]
		kcp.sendUna = seg.sn
	} else {
		// Send buf is empty, it means all packets have been acknowledged by remote.
		kcp.sendUna = kcp.sendNext
	}
}

// processAck marks the segment with the same sequence number in the send buf as acked.
func (kcp *KCP) processAck(sn uint32) {
	// If sequence number is either too old or too new, drop it.
	if timediff(sn, kcp.sendUna) < 0 || timediff(sn, kcp.sendNext) >= 0 {
		return
	}

	for k := range kcp.sendBuf {
		seg := &kcp.sendBuf[k]
		if timediff(seg.sn, sn) > 0 {
			// Already passed the sequence number.
			break
		}
		if sn == seg.sn {
			// Mark and free space, but leave the segment here,
			// and wait until `una` to delete this, then we don't
			// have to shift the segments across the slice,
			// which is an expensive operation for large window.
			seg.acked = true
			kcp.delSegment(seg)

			// Update congestion window.
			kcp.sendAlgorithm.OnAck()

			break
		}
	}
}

// processFastAck iterates each segment in send buf. If a segment is after
// the ACK sequence number, then increase its `fastAck` counter by 1.
func (kcp *KCP) processFastAck(sn, ts uint32) {
	// If sequence number is either too old or too new, drop it.
	if timediff(sn, kcp.sendUna) < 0 || timediff(sn, kcp.sendNext) >= 0 {
		return
	}

	fastAckLimit := int(math.Sqrt(float64(len(kcp.sendBuf)))) + 1
	fastAckCount := 0
	for k := range kcp.sendBuf {
		seg := &kcp.sendBuf[k]
		if timediff(seg.sn, sn) > 0 {
			// Already passed the sequence number.
			break
		}
		if sn != seg.sn && timediff(seg.ts, ts) <= 0 {
			seg.fastAck++
			fastAckCount++
		}
		if fastAckCount >= fastAckLimit {
			break
		}
	}
}

// Based on the unacknowledged number, it removes all the segments in the send buf
// that is acknowledged (sequence number smaller than unacknowledged number).
// Returns the number of segments removed.
func (kcp *KCP) removeAckedPktsFromSendBuf(una uint32) int {
	rm_count := 0
	for k := range kcp.sendBuf {
		seg := &kcp.sendBuf[k]
		if timediff(una, seg.sn) > 0 {
			// Any segment in the send buf that is smaller than
			// the unacknowledged number can be removed.
			kcp.delSegment(seg)
			rm_count++
		} else {
			break
		}
	}
	if rm_count > 0 {
		kcp.sendBuf = kcp.removeFront(kcp.sendBuf, rm_count)
	}
	return rm_count
}

// appendToAckList appends the sequence number and timestamp to ACK list.
func (kcp *KCP) appendToAckList(sn, ts uint32) {
	kcp.ackList = append(kcp.ackList, ackItem{sn, ts})
}

// processReceivedData adds the newly received segment into receive buf.
// Returns true if data is repeated.
func (kcp *KCP) processReceivedData(newseg segment) bool {
	sn := newseg.sn
	// If sequence number is either too old or too new, drop it.
	if timediff(sn, kcp.recvNext+uint32(kcp.recvWindow)) >= 0 || timediff(sn, kcp.recvNext) < 0 {
		return true
	}

	n := len(kcp.recvBuf) - 1
	insertIdx := 0
	repeat := false
	for i := n; i >= 0; i-- {
		seg := &kcp.recvBuf[i]
		if seg.sn == sn {
			repeat = true
			break
		}
		if timediff(seg.sn, sn) < 0 {
			// Found the place in the receive buf to insert the new segment.
			insertIdx = i + 1
			break
		}
	}

	// Insert the segment only when it is not repeated.
	if !repeat {
		dataCopy := PktCachePool.Get()[:len(newseg.data)]
		copy(dataCopy, newseg.data)
		newseg.data = dataCopy

		if insertIdx == n+1 {
			kcp.recvBuf = append(kcp.recvBuf, newseg)
		} else {
			kcp.recvBuf = append(kcp.recvBuf, segment{})
			copy(kcp.recvBuf[insertIdx+1:], kcp.recvBuf[insertIdx:])
			kcp.recvBuf[insertIdx] = newseg
		}
	}

	// Move available data from receive buf to receive queue.
	rmCount := 0
	for k := range kcp.recvBuf {
		seg := &kcp.recvBuf[k]
		if seg.sn == kcp.recvNext {
			kcp.recvNext++
			rmCount++
		} else {
			break
		}
	}
	if rmCount > 0 {
		kcp.recvQueue = append(kcp.recvQueue, kcp.recvBuf[:rmCount]...)
		kcp.recvBuf = kcp.removeFront(kcp.recvBuf, rmCount)
	}

	return repeat
}

// removeFront removes front n elements from the given segment buf / queue.
func (kcp *KCP) removeFront(q []segment, n int) []segment {
	if n > cap(q)/2 {
		n2 := copy(q, q[n:])
		return q[:n2]
	}
	return q[n:]
}
