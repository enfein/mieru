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

package appctl

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"

	pb "github.com/enfein/mieru/pkg/appctl/appctlpb"
	"google.golang.org/protobuf/proto"
)

var (
	validPortRange = regexp.MustCompile(`^(\d+)-(\d+)$`)
)

// FlatPortBindings checks port bindings and convert port range to a list of ports.
func FlatPortBindings(bindings []*pb.PortBinding) ([]*pb.PortBinding, error) {
	res := make([]*pb.PortBinding, 0)
	if len(bindings) == 0 {
		return res, nil
	}
	tcp := make(map[int32]struct{})
	udp := make(map[int32]struct{})
	for _, binding := range bindings {
		if binding.GetProtocol() == pb.TransportProtocol_UNKNOWN_TRANSPORT_PROTOCOL {
			return res, fmt.Errorf("protocol is not set")
		}
		if binding.GetPort() != 0 {
			if binding.GetPort() < 1 || binding.GetPort() > 65535 {
				return res, fmt.Errorf("port number %d is invalid", binding.GetPort())
			}
			switch binding.GetProtocol() {
			case pb.TransportProtocol_TCP:
				tcp[binding.GetPort()] = struct{}{}
			case pb.TransportProtocol_UDP:
				udp[binding.GetPort()] = struct{}{}
			default:
				return res, fmt.Errorf("unknown protocol %s", binding.GetProtocol().String())
			}
		} else {
			matches := validPortRange.FindStringSubmatch(binding.GetPortRange())
			if len(matches) != 3 {
				return res, fmt.Errorf("unable to parse port range %q", binding.GetPortRange())
			}
			small, err := strconv.Atoi(matches[1])
			if err != nil {
				return res, fmt.Errorf("unable to parse int from %q", matches[1])
			}
			big, err := strconv.Atoi(matches[2])
			if err != nil {
				return res, fmt.Errorf("unable to parse int from %q", matches[2])
			}
			if small < 1 || small > 65535 {
				return res, fmt.Errorf("port number %d is invalid", small)
			}
			if big < 1 || big > 65535 {
				return res, fmt.Errorf("port number %d is invalid", big)
			}
			if small > big {
				return res, fmt.Errorf("begin of port range %d is bigger than end of port range %d", small, big)
			}
			switch binding.GetProtocol() {
			case pb.TransportProtocol_TCP:
				for i := small; i <= big; i++ {
					tcp[int32(i)] = struct{}{}
				}
			case pb.TransportProtocol_UDP:
				for i := small; i <= big; i++ {
					udp[int32(i)] = struct{}{}
				}
			default:
				return res, fmt.Errorf("unknown protocol %s", binding.GetProtocol().String())
			}
		}
	}
	tcpList := make([]int32, 0)
	udpList := make([]int32, 0)
	for port := range tcp {
		tcpList = append(tcpList, port)
	}
	for port := range udp {
		udpList = append(udpList, port)
	}
	sort.Slice(tcpList, func(i, j int) bool { return tcpList[i] < tcpList[j] })
	sort.Slice(udpList, func(i, j int) bool { return udpList[i] < udpList[j] })
	for _, port := range tcpList {
		res = append(res, &pb.PortBinding{
			Port:     proto.Int32(port),
			Protocol: pb.TransportProtocol_TCP.Enum(),
		})
	}
	for _, port := range udpList {
		res = append(res, &pb.PortBinding{
			Port:     proto.Int32(port),
			Protocol: pb.TransportProtocol_UDP.Enum(),
		})
	}
	return res, nil
}
