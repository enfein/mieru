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

	// Map the SessionInfo object to fields, and record the length of the fields.
	table := make([][]string, 0)
	table = append(table, header)
	for _, si := range info.GetItems() {
		row := make([]string, 9)

		row[0] = si.GetId()
		row[1] = si.GetProtocol()
		row[2] = si.GetLocalAddr()
		row[3] = si.GetRemoteAddr()
		row[4] = si.GetState()
		row[5] = fmt.Sprintf("%d+%d", si.GetRecvQ(), si.GetRecvBuf())
		row[6] = fmt.Sprintf("%d+%d", si.GetSendQ(), si.GetSendBuf())

		lastRecvTime := time.Unix(si.LastRecvTime.GetSeconds(), int64(si.LastRecvTime.GetNanos()))
		if si.GetProtocol() == "TCP" {
			row[7] = fmt.Sprintf("%v", time.Since(lastRecvTime).Truncate(time.Second))
		} else {
			row[7] = fmt.Sprintf("%v (%d)", time.Since(lastRecvTime).Truncate(time.Second), si.GetLastRecvSeq())
		}
		lastSendTime := time.Unix(si.LastSendTime.GetSeconds(), int64(si.LastSendTime.GetNanos()))
		row[8] = fmt.Sprintf("%v (%d)", time.Since(lastSendTime).Truncate(time.Second), si.GetLastSendSeq())

		table = append(table, row)
	}

	printTable(table, "  ")
}

func printTable(table [][]string, delim string) {
	nRow := len(table)
	if nRow == 0 {
		return
	}
	nCol := len(table[0])
	if nCol == 0 {
		return
	}

	// Verify each row has the same number of columns.
	for _, row := range table {
		if len(row) != nCol {
			panic(fmt.Sprintf("when print table, row %v has %d columns, expect %d", row, len(row), nCol))
		}
	}

	// Calculate the length that should occupy by each column.
	lens := make([]int, nCol)
	for _, row := range table {
		for j, field := range row {
			lens[j] = mathext.Max(lens[j], len(field))
		}
	}

	// Print the table with padding.
	for _, row := range table {
		rowWithPadding := make([]string, nCol)
		for j := range row {
			rowWithPadding[j] = fmt.Sprintf("%-"+fmt.Sprintf("%d", lens[j])+"s", row[j])
		}
		log.Infof("%s", strings.Join(rowWithPadding, delim))
	}
}
