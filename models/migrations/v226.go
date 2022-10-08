// Copyright 2022 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package migrations

import (
	"xorm.io/builder"
	"xorm.io/xorm"
)

func fixPackageSemverField(x *xorm.Engine) error {
	_, err := x.Exec(builder.Update(builder.Eq{"semver_compatible": false}).From("`package`").Where(builder.In("`type`", "conan", "generic")))
	return err
}
