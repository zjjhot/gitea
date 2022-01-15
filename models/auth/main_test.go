// Copyright 2020 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package auth

import (
	"path/filepath"
	"testing"

	"code.gitea.io/gitea/models/unittest"
)

func TestMain(m *testing.M) {
	unittest.MainTest(m, filepath.Join("..", ".."),
		"login_source.yml",
		"oauth2_application.yml",
		"oauth2_authorization_code.yml",
		"oauth2_grant.yml",
		"webauthn_credential.yml",
	)
}
