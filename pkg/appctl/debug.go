// Copyright (C) 2025  mieru authors
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

package appctl

import (
	"os"

	pb "github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
	"github.com/enfein/mieru/v3/pkg/common"
)

// EnableAllGRPCLog let gRPC to print all internal logs.
func EnableAllGRPCLog() {
	os.Setenv("GRPC_GO_LOG_VERBOSITY_LEVEL", "99")
	os.Setenv("GRPC_GO_LOG_SEVERITY_LEVEL", "info")
}

func getMemoryStatistics() (*pb.MemoryStatistics, error) {
	ms := common.GetMemoryStats()
	return &pb.MemoryStatistics{
		HeapBytes:       &ms.HeapAlloc,
		HeapObjects:     &ms.HeapObjects,
		MaxHeapBytes:    &ms.HeapSys,
		TargetHeapBytes: &ms.NextGC,
		StackBytes:      &ms.StackSys,
	}, nil
}
