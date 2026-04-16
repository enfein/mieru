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

package cli

import (
	"encoding/hex"
	"fmt"

	"github.com/enfein/mieru/v3/pkg/cipher/noise"
)

// keypairNoiseFunc handles the CLI command
//
//	{mieru|mita} keypair noise [curve]
//
// It generates a fresh long-term static keypair for the chosen DH
// function (currently only 25519) and prints both values as hex to
// stdout. The output is intentionally minimal so it can be pasted
// into configuration files or piped into other tooling:
//
//	private: 72ef...
//	public:  8ea1...
//
// This command does not interact with any running mieru process and
// has no side effects beyond writing to stdout.
func keypairNoiseFunc(args []string) error {
	// args layout: [binary, "keypair", "noise", (optional curve name)]
	curve := "25519"
	if len(args) == 4 {
		curve = args[3]
	}
	dh, err := noise.ParseDH(curve)
	if err != nil {
		return fmt.Errorf("unsupported curve %q: %w", curve, err)
	}

	kp, err := noise.GenerateKeypair(dh)
	if err != nil {
		return fmt.Errorf("GenerateKeypair: %w", err)
	}
	fmt.Printf("private: %s\n", hex.EncodeToString(kp.Private))
	fmt.Printf("public:  %s\n", hex.EncodeToString(kp.Public))
	return nil
}

// keypairNoiseValidator accepts either `keypair noise` or
// `keypair noise <curve>`.
func keypairNoiseValidator(args []string) error {
	if len(args) != 3 && len(args) != 4 {
		return fmt.Errorf("usage: %s keypair noise [curve]", args[0])
	}
	return nil
}

// RegisterKeypairNoiseCommand wires the shared `keypair noise` command
// into the given CLI binary. Called by both RegisterClientCommands and
// RegisterServerCommands so `mieru keypair noise` and
// `mita keypair noise` behave identically.
func RegisterKeypairNoiseCommand() {
	RegisterCallback(
		[]string{"", "keypair", "noise"},
		keypairNoiseValidator,
		keypairNoiseFunc,
	)
}
