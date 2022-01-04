// Copyright 2021 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package auth

import (
	"strings"

	"code.gitea.io/gitea/models/auth"
	"code.gitea.io/gitea/models/db"
	user_model "code.gitea.io/gitea/models/user"
	"code.gitea.io/gitea/modules/log"
	"code.gitea.io/gitea/services/auth/source/oauth2"
	"code.gitea.io/gitea/services/auth/source/smtp"

	_ "code.gitea.io/gitea/services/auth/source/db"   // register the sources (and below)
	_ "code.gitea.io/gitea/services/auth/source/ldap" // register the ldap source
	_ "code.gitea.io/gitea/services/auth/source/pam"  // register the pam source
	_ "code.gitea.io/gitea/services/auth/source/sspi" // register the sspi source
)

// UserSignIn validates user name and password.
func UserSignIn(username, password string) (*user_model.User, *auth.Source, error) {
	var user *user_model.User
	if strings.Contains(username, "@") {
		user = &user_model.User{Email: strings.ToLower(strings.TrimSpace(username))}
		// check same email
		cnt, err := db.Count(user)
		if err != nil {
			return nil, nil, err
		}
		if cnt > 1 {
			return nil, nil, user_model.ErrEmailAlreadyUsed{
				Email: user.Email,
			}
		}
	} else {
		trimmedUsername := strings.TrimSpace(username)
		if len(trimmedUsername) == 0 {
			return nil, nil, user_model.ErrUserNotExist{Name: username}
		}

		user = &user_model.User{LowerName: strings.ToLower(trimmedUsername)}
	}

	hasUser, err := user_model.GetUser(user)
	if err != nil {
		return nil, nil, err
	}

	if hasUser {
		source, err := auth.GetSourceByID(user.LoginSource)
		if err != nil {
			return nil, nil, err
		}

		if !source.IsActive {
			return nil, nil, oauth2.ErrAuthSourceNotActived
		}

		authenticator, ok := source.Cfg.(PasswordAuthenticator)
		if !ok {
			return nil, nil, smtp.ErrUnsupportedLoginType
		}

		user, err := authenticator.Authenticate(user, username, password)
		if err != nil {
			return nil, nil, err
		}

		// WARN: DON'T check user.IsActive, that will be checked on reqSign so that
		// user could be hint to resend confirm email.
		if user.ProhibitLogin {
			return nil, nil, user_model.ErrUserProhibitLogin{UID: user.ID, Name: user.Name}
		}

		return user, source, nil
	}

	sources, err := auth.AllActiveSources()
	if err != nil {
		return nil, nil, err
	}

	for _, source := range sources {
		if !source.IsActive {
			// don't try to authenticate non-active sources
			continue
		}

		authenticator, ok := source.Cfg.(PasswordAuthenticator)
		if !ok {
			continue
		}

		authUser, err := authenticator.Authenticate(nil, username, password)

		if err == nil {
			if !authUser.ProhibitLogin {
				return authUser, source, nil
			}
			err = user_model.ErrUserProhibitLogin{UID: authUser.ID, Name: authUser.Name}
		}

		if user_model.IsErrUserNotExist(err) {
			log.Debug("Failed to login '%s' via '%s': %v", username, source.Name, err)
		} else {
			log.Warn("Failed to login '%s' via '%s': %v", username, source.Name, err)
		}
	}

	return nil, nil, user_model.ErrUserNotExist{Name: username}
}
