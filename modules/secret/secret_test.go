// Copyright 2019 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package secret

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEncryptDecrypt(t *testing.T) {
	var hex string
	var str string

	hex, _ = EncryptSecret("foo", "baz")
	str, _ = DecryptSecret("foo", hex)
	assert.Equal(t, str, "baz")

	hex, _ = EncryptSecret("bar", "baz")
	str, _ = DecryptSecret("foo", hex)
	assert.NotEqual(t, str, "baz")
}
