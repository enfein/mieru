// Copyright (C) 2023  mieru authors
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

package cli

import (
	"fmt"
	"runtime/debug"
	"strings"
	"time"

	"github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/log"
	"github.com/enfein/mieru/v3/pkg/mathext"
	"github.com/enfein/mieru/v3/pkg/version"
)

var versionFunc = func(_ []string) error {
	log.Infof(version.AppVersion)
	return nil
}

var describeBuildFunc = func(_ []string) error {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return fmt.Errorf("build info is unavailable")
	}
	log.Infof(info.String())
	return nil
}

func printSessionInfoList(info *appctlpb.SessionInfoList) {
	header := []string{
		"SessionID",
		"Protocol",
		"Local",
		"Remote",
		"State",
		"RecvQ+Buf",
		"SendQ+Buf",
		"LastRecv",
		"LastSend",
	}
	idLen := len(header[0])
	protocolLen := len(header[1])
	localAddrLen := len(header[2])
	remoteAddrLen := len(header[3])
	stateLen := len(header[4])
	recvQBufLen := len(header[5])
	sendQBufLen := len(header[6])
	lastRecvLen := len(header[7])
	lastSendLen := len(header[8])

	// Map the SessionInfo object to fields, and record the length of the fields.
	table := make([][]string, 0)
	table = append(table, header)
	for _, si := range info.GetItems() {
		row := make([]string, 9)

		row[0] = si.GetId()
		idLen = mathext.Max(idLen, len(row[0]))
		row[1] = si.GetProtocol()
		protocolLen = mathext.Max(protocolLen, len(row[1]))
		row[2] = si.GetLocalAddr()
		localAddrLen = mathext.Max(localAddrLen, len(row[2]))
		row[3] = si.GetRemoteAddr()
		remoteAddrLen = mathext.Max(remoteAddrLen, len(row[3]))
		row[4] = si.GetState()
		stateLen = mathext.Max(stateLen, len(row[4]))

		row[5] = fmt.Sprintf("%d+%d", si.GetRecvQ(), si.GetRecvBuf())
		recvQBufLen = mathext.Max(recvQBufLen, len(row[5]))
		row[6] = fmt.Sprintf("%d+%d", si.GetSendQ(), si.GetSendBuf())
		sendQBufLen = mathext.Max(sendQBufLen, len(row[6]))

		lastRecvTime := time.Unix(si.LastRecvTime.GetSeconds(), int64(si.LastRecvTime.GetNanos()))
		if si.GetProtocol() == "TCP" {
			row[7] = fmt.Sprintf("%v", time.Since(lastRecvTime).Truncate(time.Second))
		} else {
			row[7] = fmt.Sprintf("%v (%d)", time.Since(lastRecvTime).Truncate(time.Second), si.GetLastRecvSeq())
		}
		lastRecvLen = mathext.Max(lastRecvLen, len(row[7]))
		lastSendTime := time.Unix(si.LastSendTime.GetSeconds(), int64(si.LastSendTime.GetNanos()))
		row[8] = fmt.Sprintf("%v (%d)", time.Since(lastSendTime).Truncate(time.Second), si.GetLastSendSeq())
		lastSendLen = mathext.Max(lastSendLen, len(row[8]))

		table = append(table, row)
	}

	// Pad the length of each row and print.
	delim := "  "
	for _, row := range table {
		rowWithPadding := make([]string, 9)
		rowWithPadding[0] = fmt.Sprintf("%-"+fmt.Sprintf("%d", idLen)+"s", row[0])
		rowWithPadding[1] = fmt.Sprintf("%-"+fmt.Sprintf("%d", protocolLen)+"s", row[1])
		rowWithPadding[2] = fmt.Sprintf("%-"+fmt.Sprintf("%d", localAddrLen)+"s", row[2])
		rowWithPadding[3] = fmt.Sprintf("%-"+fmt.Sprintf("%d", remoteAddrLen)+"s", row[3])
		rowWithPadding[4] = fmt.Sprintf("%-"+fmt.Sprintf("%d", stateLen)+"s", row[4])
		rowWithPadding[5] = fmt.Sprintf("%-"+fmt.Sprintf("%d", recvQBufLen)+"s", row[5])
		rowWithPadding[6] = fmt.Sprintf("%-"+fmt.Sprintf("%d", sendQBufLen)+"s", row[6])
		rowWithPadding[7] = fmt.Sprintf("%-"+fmt.Sprintf("%d", lastRecvLen)+"s", row[7])
		rowWithPadding[8] = fmt.Sprintf("%-"+fmt.Sprintf("%d", lastSendLen)+"s", row[8])
		log.Infof("%s", strings.Join(rowWithPadding, delim))
	}
}
