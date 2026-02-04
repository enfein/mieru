// Copyright (C) 2026  mieru authors
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

package appctlcommon

import (
	"encoding/hex"
	"fmt"

	pb "github.com/enfein/mieru/v3/pkg/appctl/appctlpb"
)

func ValidateTrafficPattern(pattern *pb.TrafficPattern) error {
	if pattern == nil {
		return nil
	}
	if err := validateTCPFragment(pattern.GetTcpFragment()); err != nil {
		return err
	}
	if err := validateNoncePattern(pattern.GetNonce()); err != nil {
		return err
	}
	return nil
}

func validateTCPFragment(fragment *pb.TCPFragment) error {
	if fragment == nil {
		return nil
	}
	if fragment.MaxSleepMs != nil {
		if fragment.GetMaxSleepMs() < 0 {
			return fmt.Errorf("TCPFragment maxSleepMs %d is negative", fragment.GetMaxSleepMs())
		}
		if fragment.GetMaxSleepMs() > 100 {
			return fmt.Errorf("TCPFragment maxSleepMs %d exceeds maximum value 100", fragment.GetMaxSleepMs())
		}
	}
	return nil
}

func validateNoncePattern(nonce *pb.NoncePattern) error {
	if nonce == nil {
		return nil
	}
	if nonce.MinLen != nil {
		if nonce.GetMinLen() < 0 {
			return fmt.Errorf("NoncePattern minLen %d is negative", nonce.GetMinLen())
		}
		if nonce.GetMinLen() > 12 {
			return fmt.Errorf("NoncePattern minLen %d exceeds maximum value 12", nonce.GetMinLen())
		}
	}
	if nonce.MaxLen != nil {
		if nonce.GetMaxLen() < 0 {
			return fmt.Errorf("NoncePattern maxLen %d is negative", nonce.GetMaxLen())
		}
		if nonce.GetMaxLen() > 12 {
			return fmt.Errorf("NoncePattern maxLen %d exceeds maximum value 12", nonce.GetMaxLen())
		}
	}
	if nonce.MinLen != nil && nonce.MaxLen != nil {
		if nonce.GetMinLen() > nonce.GetMaxLen() {
			return fmt.Errorf("NoncePattern minLen %d is greater than maxLen %d", nonce.GetMinLen(), nonce.GetMaxLen())
		}
	}
	for i, hexStr := range nonce.GetCustomHexStrings() {
		decoded, err := hex.DecodeString(hexStr)
		if err != nil {
			return fmt.Errorf("NoncePattern customHexStrings[%d] %q is not a valid hex string: %w", i, hexStr, err)
		}
		if len(decoded) > 12 {
			return fmt.Errorf("NoncePattern customHexStrings[%d] decoded length %d exceeds maximum 12 bytes", i, len(decoded))
		}
	}
	return nil
}
