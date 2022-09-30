// Copyright 2018 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.package models

package integration

import (
	"fmt"
	"net/http"
	"testing"

	"code.gitea.io/gitea/models/unittest"
	user_model "code.gitea.io/gitea/models/user"
	api "code.gitea.io/gitea/modules/structs"
	"code.gitea.io/gitea/tests"

	"github.com/stretchr/testify/assert"
)

func TestUserOrgs(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	adminUsername := "user1"
	normalUsername := "user2"
	privateMemberUsername := "user4"
	unrelatedUsername := "user5"

	orgs := getUserOrgs(t, adminUsername, normalUsername)

	user3 := unittest.AssertExistsAndLoadBean(t, &user_model.User{Name: "user3"})
	user17 := unittest.AssertExistsAndLoadBean(t, &user_model.User{Name: "user17"})

	assert.Equal(t, []*api.Organization{
		{
			ID:          17,
			Name:        user17.Name,
			UserName:    user17.Name,
			FullName:    user17.FullName,
			AvatarURL:   user17.AvatarLink(),
			Description: "",
			Website:     "",
			Location:    "",
			Visibility:  "public",
		},
		{
			ID:          3,
			Name:        user3.Name,
			UserName:    user3.Name,
			FullName:    user3.FullName,
			AvatarURL:   user3.AvatarLink(),
			Description: "",
			Website:     "",
			Location:    "",
			Visibility:  "public",
		},
	}, orgs)

	// user itself should get it's org's he is a member of
	orgs = getUserOrgs(t, privateMemberUsername, privateMemberUsername)
	assert.Len(t, orgs, 1)

	// unrelated user should not get private org membership of privateMemberUsername
	orgs = getUserOrgs(t, unrelatedUsername, privateMemberUsername)
	assert.Len(t, orgs, 0)

	// not authenticated call also should hide org membership
	orgs = getUserOrgs(t, "", privateMemberUsername)
	assert.Len(t, orgs, 0)
}

func getUserOrgs(t *testing.T, userDoer, userCheck string) (orgs []*api.Organization) {
	token := ""
	session := emptyTestSession(t)
	if len(userDoer) != 0 {
		session = loginUser(t, userDoer)
		token = getTokenForLoggedInUser(t, session)
	}
	urlStr := fmt.Sprintf("/api/v1/users/%s/orgs?token=%s", userCheck, token)
	req := NewRequest(t, "GET", urlStr)
	resp := session.MakeRequest(t, req, http.StatusOK)
	DecodeJSON(t, resp, &orgs)
	return orgs
}

func TestMyOrgs(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	session := emptyTestSession(t)
	req := NewRequest(t, "GET", "/api/v1/user/orgs")
	session.MakeRequest(t, req, http.StatusUnauthorized)

	normalUsername := "user2"
	session = loginUser(t, normalUsername)
	token := getTokenForLoggedInUser(t, session)
	req = NewRequest(t, "GET", "/api/v1/user/orgs?token="+token)
	resp := session.MakeRequest(t, req, http.StatusOK)
	var orgs []*api.Organization
	DecodeJSON(t, resp, &orgs)
	user3 := unittest.AssertExistsAndLoadBean(t, &user_model.User{Name: "user3"})
	user17 := unittest.AssertExistsAndLoadBean(t, &user_model.User{Name: "user17"})

	assert.Equal(t, []*api.Organization{
		{
			ID:          17,
			Name:        user17.Name,
			UserName:    user17.Name,
			FullName:    user17.FullName,
			AvatarURL:   user17.AvatarLink(),
			Description: "",
			Website:     "",
			Location:    "",
			Visibility:  "public",
		},
		{
			ID:          3,
			Name:        user3.Name,
			UserName:    user3.Name,
			FullName:    user3.FullName,
			AvatarURL:   user3.AvatarLink(),
			Description: "",
			Website:     "",
			Location:    "",
			Visibility:  "public",
		},
	}, orgs)
}
