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

package egress

import "github.com/enfein/mieru/pkg/appctl/appctlpb"

type Input struct {
	Protocol appctlpb.ProxyProtocol
	Data     []byte
}

type Action struct {
	Action appctlpb.EgressAction
	Proxy  *appctlpb.EgressProxy
}

type Controller interface {
	// FindAction returns the action to do based on the input data.
	FindAction(in Input) Action
}

// AlwaysDirectController always returns DIRECT action.
type AlwaysDirectController struct{}

var (
	_ Controller = AlwaysDirectController{}
)

func (c AlwaysDirectController) FindAction(in Input) Action {
	return Action{
		Action: appctlpb.EgressAction_DIRECT,
	}
}
