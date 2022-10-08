// Copyright 2020 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package markdown

import (
	"fmt"
	"strings"
	"testing"

	"code.gitea.io/gitea/modules/structs"

	"github.com/stretchr/testify/assert"
)

func validateMetadata(it structs.IssueTemplate) bool {
	/*
		A legacy to keep the unit tests working.
		Copied from the method "func (it IssueTemplate) Valid() bool", the original method has been removed.
		Because it becomes quite complicated to validate an issue template which is support yaml form now.
		The new way to validate an issue template is to call the Validate in modules/issue/template,
	*/
	return strings.TrimSpace(it.Name) != "" && strings.TrimSpace(it.About) != ""
}

func TestExtractMetadata(t *testing.T) {
	t.Run("ValidFrontAndBody", func(t *testing.T) {
		var meta structs.IssueTemplate
		body, err := ExtractMetadata(fmt.Sprintf("%s\n%s\n%s\n%s", sepTest, frontTest, sepTest, bodyTest), &meta)
		assert.NoError(t, err)
		assert.Equal(t, bodyTest, body)
		assert.Equal(t, metaTest, meta)
		assert.True(t, validateMetadata(meta))
	})

	t.Run("NoFirstSeparator", func(t *testing.T) {
		var meta structs.IssueTemplate
		_, err := ExtractMetadata(fmt.Sprintf("%s\n%s\n%s", frontTest, sepTest, bodyTest), &meta)
		assert.Error(t, err)
	})

	t.Run("NoLastSeparator", func(t *testing.T) {
		var meta structs.IssueTemplate
		_, err := ExtractMetadata(fmt.Sprintf("%s\n%s\n%s", sepTest, frontTest, bodyTest), &meta)
		assert.Error(t, err)
	})

	t.Run("NoBody", func(t *testing.T) {
		var meta structs.IssueTemplate
		body, err := ExtractMetadata(fmt.Sprintf("%s\n%s\n%s", sepTest, frontTest, sepTest), &meta)
		assert.NoError(t, err)
		assert.Equal(t, "", body)
		assert.Equal(t, metaTest, meta)
		assert.True(t, validateMetadata(meta))
	})
}

func TestExtractMetadataBytes(t *testing.T) {
	t.Run("ValidFrontAndBody", func(t *testing.T) {
		var meta structs.IssueTemplate
		body, err := ExtractMetadataBytes([]byte(fmt.Sprintf("%s\n%s\n%s\n%s", sepTest, frontTest, sepTest, bodyTest)), &meta)
		assert.NoError(t, err)
		assert.Equal(t, bodyTest, string(body))
		assert.Equal(t, metaTest, meta)
		assert.True(t, validateMetadata(meta))
	})

	t.Run("NoFirstSeparator", func(t *testing.T) {
		var meta structs.IssueTemplate
		_, err := ExtractMetadataBytes([]byte(fmt.Sprintf("%s\n%s\n%s", frontTest, sepTest, bodyTest)), &meta)
		assert.Error(t, err)
	})

	t.Run("NoLastSeparator", func(t *testing.T) {
		var meta structs.IssueTemplate
		_, err := ExtractMetadataBytes([]byte(fmt.Sprintf("%s\n%s\n%s", sepTest, frontTest, bodyTest)), &meta)
		assert.Error(t, err)
	})

	t.Run("NoBody", func(t *testing.T) {
		var meta structs.IssueTemplate
		body, err := ExtractMetadataBytes([]byte(fmt.Sprintf("%s\n%s\n%s", sepTest, frontTest, sepTest)), &meta)
		assert.NoError(t, err)
		assert.Equal(t, "", string(body))
		assert.Equal(t, metaTest, meta)
		assert.True(t, validateMetadata(meta))
	})
}

var (
	sepTest   = "-----"
	frontTest = `name: Test
about: "A Test"
title: "Test Title"
labels:
  - bug
  - "test label"`
	bodyTest = "This is the body"
	metaTest = structs.IssueTemplate{
		Name:   "Test",
		About:  "A Test",
		Title:  "Test Title",
		Labels: []string{"bug", "test label"},
	}
)
