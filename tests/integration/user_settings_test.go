// Copyright 2024 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"net/http"
	"testing"

	"code.gitea.io/gitea/modules/container"
	"code.gitea.io/gitea/modules/setting"
	"code.gitea.io/gitea/tests"
)

// Validate that each navbar setting is correct. This checks that the
// appropriate context is passed everywhere the navbar is rendered
func assertNavbar(t *testing.T, doc *HTMLDoc) {
	// Only show the account page if users can change their email notifications, delete themselves, or manage credentials
	if setting.Admin.UserDisabledFeatures.Contains(setting.UserFeatureDeletion, setting.UserFeatureManageCredentials) && !setting.Service.EnableNotifyMail {
		doc.AssertElement(t, ".menu a[href='/user/settings/account']", false)
	} else {
		doc.AssertElement(t, ".menu a[href='/user/settings/account']", true)
	}

	if setting.Admin.UserDisabledFeatures.Contains(setting.UserFeatureManageMFA, setting.UserFeatureManageCredentials) {
		doc.AssertElement(t, ".menu a[href='/user/settings/security']", false)
	} else {
		doc.AssertElement(t, ".menu a[href='/user/settings/security']", true)
	}

	if setting.Admin.UserDisabledFeatures.Contains(setting.UserFeatureManageSSHKeys, setting.UserFeatureManageGPGKeys) {
		doc.AssertElement(t, ".menu a[href='/user/settings/keys']", false)
	} else {
		doc.AssertElement(t, ".menu a[href='/user/settings/keys']", true)
	}
}

func WithDisabledFeatures(t *testing.T, features ...string) {
	t.Helper()

	global := setting.Admin.UserDisabledFeatures
	user := setting.Admin.ExternalUserDisableFeatures

	setting.Admin.UserDisabledFeatures = container.SetOf(features...)
	setting.Admin.ExternalUserDisableFeatures = setting.Admin.UserDisabledFeatures

	t.Cleanup(func() {
		setting.Admin.UserDisabledFeatures = global
		setting.Admin.ExternalUserDisableFeatures = user
	})
}

func TestUserSettingsAccount(t *testing.T) {
	t.Run("all features enabled", func(t *testing.T) {
		defer tests.PrepareTestEnv(t)()

		session := loginUser(t, "user2")
		req := NewRequest(t, "GET", "/user/settings/account")
		resp := session.MakeRequest(t, req, http.StatusOK)
		doc := NewHTMLParser(t, resp.Body)

		// account navbar should display
		doc.AssertElement(t, ".menu a[href='/user/settings/account']", true)

		doc.AssertElement(t, "#password", true)
		doc.AssertElement(t, "#email", true)
		doc.AssertElement(t, "#delete-form", true)
	})

	t.Run("credentials disabled", func(t *testing.T) {
		defer tests.PrepareTestEnv(t)()

		WithDisabledFeatures(t, setting.UserFeatureManageCredentials)

		session := loginUser(t, "user2")
		req := NewRequest(t, "GET", "/user/settings/account")
		resp := session.MakeRequest(t, req, http.StatusOK)
		doc := NewHTMLParser(t, resp.Body)

		assertNavbar(t, doc)

		doc.AssertElement(t, "#password", false)
		doc.AssertElement(t, "#email", false)
		doc.AssertElement(t, "#delete-form", true)
	})

	t.Run("deletion disabled", func(t *testing.T) {
		defer tests.PrepareTestEnv(t)()

		WithDisabledFeatures(t, setting.UserFeatureDeletion)

		session := loginUser(t, "user2")
		req := NewRequest(t, "GET", "/user/settings/account")
		resp := session.MakeRequest(t, req, http.StatusOK)
		doc := NewHTMLParser(t, resp.Body)

		assertNavbar(t, doc)

		doc.AssertElement(t, "#password", true)
		doc.AssertElement(t, "#email", true)
		doc.AssertElement(t, "#delete-form", false)
	})

	t.Run("deletion, credentials and email notifications are disabled", func(t *testing.T) {
		defer tests.PrepareTestEnv(t)()

		mail := setting.Service.EnableNotifyMail
		setting.Service.EnableNotifyMail = false
		defer func() {
			setting.Service.EnableNotifyMail = mail
		}()

		WithDisabledFeatures(t, setting.UserFeatureDeletion, setting.UserFeatureManageCredentials)

		session := loginUser(t, "user2")
		req := NewRequest(t, "GET", "/user/settings/account")
		session.MakeRequest(t, req, http.StatusNotFound)
	})
}

func TestUserSettingsUpdatePassword(t *testing.T) {
	t.Run("enabled", func(t *testing.T) {
		defer tests.PrepareTestEnv(t)()

		session := loginUser(t, "user2")

		req := NewRequest(t, "GET", "/user/settings/account")
		resp := session.MakeRequest(t, req, http.StatusOK)
		doc := NewHTMLParser(t, resp.Body)

		req = NewRequestWithValues(t, "POST", "/user/settings/account", map[string]string{
			"_csrf":        doc.GetCSRF(),
			"old_password": "password",
			"password":     "password",
			"retype":       "password",
		})
		session.MakeRequest(t, req, http.StatusSeeOther)
	})

	t.Run("credentials disabled", func(t *testing.T) {
		defer tests.PrepareTestEnv(t)()

		WithDisabledFeatures(t, setting.UserFeatureManageCredentials)

		session := loginUser(t, "user2")

		req := NewRequest(t, "GET", "/user/settings/account")
		resp := session.MakeRequest(t, req, http.StatusOK)
		doc := NewHTMLParser(t, resp.Body)

		req = NewRequestWithValues(t, "POST", "/user/settings/account", map[string]string{
			"_csrf": doc.GetCSRF(),
		})
		session.MakeRequest(t, req, http.StatusNotFound)
	})
}

func TestUserSettingsUpdateEmail(t *testing.T) {
	t.Run("credentials disabled", func(t *testing.T) {
		defer tests.PrepareTestEnv(t)()

		WithDisabledFeatures(t, setting.UserFeatureManageCredentials)

		session := loginUser(t, "user2")

		req := NewRequest(t, "GET", "/user/settings/account")
		resp := session.MakeRequest(t, req, http.StatusOK)
		doc := NewHTMLParser(t, resp.Body)

		req = NewRequestWithValues(t, "POST", "/user/settings/account/email", map[string]string{
			"_csrf": doc.GetCSRF(),
		})
		session.MakeRequest(t, req, http.StatusNotFound)
	})
}

func TestUserSettingsDeleteEmail(t *testing.T) {
	t.Run("credentials disabled", func(t *testing.T) {
		defer tests.PrepareTestEnv(t)()

		WithDisabledFeatures(t, setting.UserFeatureManageCredentials)

		session := loginUser(t, "user2")

		req := NewRequest(t, "GET", "/user/settings/account")
		resp := session.MakeRequest(t, req, http.StatusOK)
		doc := NewHTMLParser(t, resp.Body)

		req = NewRequestWithValues(t, "POST", "/user/settings/account/email/delete", map[string]string{
			"_csrf": doc.GetCSRF(),
		})
		session.MakeRequest(t, req, http.StatusNotFound)
	})
}

func TestUserSettingsDelete(t *testing.T) {
	t.Run("deletion disabled", func(t *testing.T) {
		defer tests.PrepareTestEnv(t)()

		WithDisabledFeatures(t, setting.UserFeatureDeletion)

		session := loginUser(t, "user2")

		req := NewRequest(t, "GET", "/user/settings/account")
		resp := session.MakeRequest(t, req, http.StatusOK)
		doc := NewHTMLParser(t, resp.Body)

		req = NewRequestWithValues(t, "POST", "/user/settings/account/delete", map[string]string{
			"_csrf": doc.GetCSRF(),
		})
		session.MakeRequest(t, req, http.StatusNotFound)
	})
}

func TestUserSettingsAppearance(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	session := loginUser(t, "user2")
	req := NewRequest(t, "GET", "/user/settings/appearance")
	resp := session.MakeRequest(t, req, http.StatusOK)
	doc := NewHTMLParser(t, resp.Body)

	assertNavbar(t, doc)
}

func TestUserSettingsSecurity(t *testing.T) {
	t.Run("credentials disabled", func(t *testing.T) {
		defer tests.PrepareTestEnv(t)()

		WithDisabledFeatures(t, setting.UserFeatureManageCredentials)

		session := loginUser(t, "user2")
		req := NewRequest(t, "GET", "/user/settings/security")
		resp := session.MakeRequest(t, req, http.StatusOK)
		doc := NewHTMLParser(t, resp.Body)

		assertNavbar(t, doc)

		doc.AssertElement(t, "#register-webauthn", true)
	})

	t.Run("mfa disabled", func(t *testing.T) {
		defer tests.PrepareTestEnv(t)()

		WithDisabledFeatures(t, setting.UserFeatureManageMFA)

		session := loginUser(t, "user2")
		req := NewRequest(t, "GET", "/user/settings/security")
		resp := session.MakeRequest(t, req, http.StatusOK)
		doc := NewHTMLParser(t, resp.Body)

		assertNavbar(t, doc)

		doc.AssertElement(t, "#register-webauthn", false)
	})

	t.Run("credentials and mfa disabled", func(t *testing.T) {
		defer tests.PrepareTestEnv(t)()

		WithDisabledFeatures(t, setting.UserFeatureManageCredentials, setting.UserFeatureManageMFA)

		session := loginUser(t, "user2")
		req := NewRequest(t, "GET", "/user/settings/security")
		session.MakeRequest(t, req, http.StatusNotFound)
	})
}

func TestUserSettingsApplications(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	session := loginUser(t, "user2")
	req := NewRequest(t, "GET", "/user/settings/applications")
	resp := session.MakeRequest(t, req, http.StatusOK)
	doc := NewHTMLParser(t, resp.Body)

	assertNavbar(t, doc)
}

func TestUserSettingsKeys(t *testing.T) {
	t.Run("all enabled", func(t *testing.T) {
		defer tests.PrepareTestEnv(t)()

		session := loginUser(t, "user2")
		req := NewRequest(t, "GET", "/user/settings/keys")
		resp := session.MakeRequest(t, req, http.StatusOK)
		doc := NewHTMLParser(t, resp.Body)

		assertNavbar(t, doc)

		doc.AssertElement(t, "#add-ssh-button", true)
		doc.AssertElement(t, "#add-gpg-key-panel", true)
	})

	t.Run("ssh keys disabled", func(t *testing.T) {
		defer tests.PrepareTestEnv(t)()

		WithDisabledFeatures(t, setting.UserFeatureManageSSHKeys)

		session := loginUser(t, "user2")
		req := NewRequest(t, "GET", "/user/settings/keys")
		resp := session.MakeRequest(t, req, http.StatusOK)
		doc := NewHTMLParser(t, resp.Body)

		assertNavbar(t, doc)

		doc.AssertElement(t, "#add-ssh-button", false)
		doc.AssertElement(t, "#add-gpg-key-panel", true)
	})

	t.Run("gpg keys disabled", func(t *testing.T) {
		defer tests.PrepareTestEnv(t)()

		WithDisabledFeatures(t, setting.UserFeatureManageGPGKeys)

		session := loginUser(t, "user2")
		req := NewRequest(t, "GET", "/user/settings/keys")
		resp := session.MakeRequest(t, req, http.StatusOK)
		doc := NewHTMLParser(t, resp.Body)

		assertNavbar(t, doc)

		doc.AssertElement(t, "#add-ssh-button", true)
		doc.AssertElement(t, "#add-gpg-key-panel", false)
	})

	t.Run("ssh & gpg keys disabled", func(t *testing.T) {
		defer tests.PrepareTestEnv(t)()

		WithDisabledFeatures(t, setting.UserFeatureManageSSHKeys, setting.UserFeatureManageGPGKeys)

		session := loginUser(t, "user2")
		req := NewRequest(t, "GET", "/user/settings/keys")
		_ = session.MakeRequest(t, req, http.StatusNotFound)
	})
}

func TestUserSettingsSecrets(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	session := loginUser(t, "user2")
	req := NewRequest(t, "GET", "/user/settings/actions/secrets")
	if setting.Actions.Enabled {
		resp := session.MakeRequest(t, req, http.StatusOK)
		doc := NewHTMLParser(t, resp.Body)

		assertNavbar(t, doc)
	} else {
		session.MakeRequest(t, req, http.StatusNotFound)
	}
}

func TestUserSettingsPackages(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	session := loginUser(t, "user2")
	req := NewRequest(t, "GET", "/user/settings/packages")
	resp := session.MakeRequest(t, req, http.StatusOK)
	doc := NewHTMLParser(t, resp.Body)

	assertNavbar(t, doc)
}

func TestUserSettingsPackagesRulesAdd(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	session := loginUser(t, "user2")
	req := NewRequest(t, "GET", "/user/settings/packages/rules/add")
	resp := session.MakeRequest(t, req, http.StatusOK)
	doc := NewHTMLParser(t, resp.Body)

	assertNavbar(t, doc)
}

func TestUserSettingsOrganization(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	session := loginUser(t, "user2")
	req := NewRequest(t, "GET", "/user/settings/organization")
	resp := session.MakeRequest(t, req, http.StatusOK)
	doc := NewHTMLParser(t, resp.Body)

	assertNavbar(t, doc)
}

func TestUserSettingsRepos(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	session := loginUser(t, "user2")
	req := NewRequest(t, "GET", "/user/settings/repos")
	resp := session.MakeRequest(t, req, http.StatusOK)
	doc := NewHTMLParser(t, resp.Body)

	assertNavbar(t, doc)
}

func TestUserSettingsBlockedUsers(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	session := loginUser(t, "user2")
	req := NewRequest(t, "GET", "/user/settings/blocked_users")
	resp := session.MakeRequest(t, req, http.StatusOK)
	doc := NewHTMLParser(t, resp.Body)

	assertNavbar(t, doc)
}
