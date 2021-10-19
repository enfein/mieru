// Copyright (C) 2021  mieru authors
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
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// jsonMarshalOption is the default option to translate protobuf into JSON.
var jsonMarshalOption = protojson.MarshalOptions{
	Multiline:       true,
	Indent:          "    ", // 4 spaces
	AllowPartial:    false,
	UseProtoNames:   false,
	UseEnumNumbers:  false,
	EmitUnpopulated: false,
}

// jsonUnmarshalOption is the default option to translate JSON into protobuf.
var jsonUnmarshalOption = protojson.UnmarshalOptions{
	AllowPartial:   false,
	DiscardUnknown: false,
}

// Marshal returns a JSON representation of protobuf.
func Marshal(m protoreflect.ProtoMessage) ([]byte, error) {
	b, err := jsonMarshalOption.Marshal(m)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// Unmarshal writes protobuf based on JSON data.
func Unmarshal(b []byte, m protoreflect.ProtoMessage) error {
	return jsonUnmarshalOption.Unmarshal(b, m)
}
