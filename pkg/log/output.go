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

package log

import (
	"io"
	"log"
	"testing"
)

// testingAdaptor let logs to be printed to testing.T.
type testingAdaptor struct {
	t *testing.T
}

var _ io.Writer = testingAdaptor{}

func (a testingAdaptor) Write(p []byte) (n int, err error) {
	s := string(p)
	a.t.Logf(s)
	return len(p), nil
}

// SetOutputToTest prints logs to the go test.
func SetOutputToTest(t *testing.T) {
	adaptor := testingAdaptor{t: t}
	log.SetOutput(adaptor)
}
