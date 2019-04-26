// Copyright 2016-2019, Pulumi Corporation.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package cmd

import (
	cryptorand "crypto/rand"
	"encoding/base64"
	"fmt"
	"os"

	"github.com/pkg/errors"

	"github.com/pulumi/pulumi/pkg/resource/config"
	"github.com/pulumi/pulumi/pkg/secrets"
	"github.com/pulumi/pulumi/pkg/secrets/passphrase"
	"github.com/pulumi/pulumi/pkg/tokens"
	"github.com/pulumi/pulumi/pkg/util/cmdutil"
	"github.com/pulumi/pulumi/pkg/util/contract"
	"github.com/pulumi/pulumi/pkg/workspace"
)

func readPassphrase(prompt string) (string, error) {
	if phrase := os.Getenv("PULUMI_CONFIG_PASSPHRASE"); phrase != "" {
		return phrase, nil
	}
	return cmdutil.ReadConsoleNoEcho(prompt)
}

func newPassphraseSecretsManager(stackName tokens.QName, configFile string) (secrets.Manager, error) {
	contract.Assertf(stackName != "", "stackName %s", "!= \"\"")

	if configFile == "" {
		f, err := workspace.DetectProjectStackPath(stackName)
		if err != nil {
			return nil, err
		}
		configFile = f
	}

	info, err := workspace.LoadProjectStack(configFile)
	if err != nil {
		return nil, err
	}

	// If we have a salt, we can just use it.
	if info.EncryptionSalt != "" {
		phrase, phraseErr := readPassphrase("Enter your passphrase to unlock config/secrets\n" +
			"    (set PULUMI_CONFIG_PASSPHRASE to remember)")
		if phraseErr != nil {
			return nil, phraseErr
		}

		sm, smerr := passphrase.NewPassphaseSecretsManager(phrase, info.EncryptionSalt)
		if smerr != nil {
			return nil, smerr
		}

		return sm, nil
	}

	// Here, the stack does not have an EncryptionSalt, so we will get a passphrase and create one
	phrase, err := readPassphrase("Enter your passphrase to protect config/secrets")
	if err != nil {
		return nil, err
	}
	confirm, err := readPassphrase("Re-enter your passphrase to confirm")
	if err != nil {
		return nil, err
	}
	if phrase != confirm {
		return nil, errors.New("passphrases do not match")
	}

	// Produce a new salt.
	salt := make([]byte, 8)
	_, err = cryptorand.Read(salt)
	contract.Assertf(err == nil, "could not read from system random")

	// Encrypt a message and store it with the salt so we can test if the password is correct later.
	crypter := config.NewSymmetricCrypterFromPassphrase(phrase, salt)
	msg, err := crypter.EncryptValue("pulumi")
	contract.AssertNoError(err)

	// Now store the result and save it.
	info.EncryptionSalt = fmt.Sprintf("v1:%s:%s", base64.StdEncoding.EncodeToString(salt), msg)
	if err = info.Save(configFile); err != nil {
		return nil, err
	}

	// Finally, build the full secrets manager from the state we just saved
	return passphrase.NewPassphaseSecretsManager(phrase, info.EncryptionSalt)
}
