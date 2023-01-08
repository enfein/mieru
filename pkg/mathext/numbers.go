// Copyright (C) 2022  mieru authors
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

package mathext

type SignedInteger interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64
}

type UnsignedInteger interface {
	~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}

type Integer interface {
	SignedInteger | UnsignedInteger
}

type Float interface {
	~float32 | ~float64
}

type Number interface {
	Integer | Float
}

// Min returns the minimum value between two input numbers.
func Min[T Number](a, b T) T {
	if a <= b {
		return a
	}
	return b
}

// Max returns the maximum value between two input numbers.
func Max[T Number](a, b T) T {
	if a >= b {
		return a
	}
	return b
}

// Mid returns the median value of three input numbers.
func Mid[T Number](a, b, c T) T {
	return Min(a, Max(b, c))
}

// Abs returns the absolute value of the input number.
func Abs[T Number](a T) T {
	if a >= 0 {
		return a
	}
	return -a
}
