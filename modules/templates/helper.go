// Copyright 2018 The Gitea Authors. All rights reserved.
// Copyright 2014 The Gogs Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package templates

import (
	"fmt"
	"html"
	"html/template"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"time"

	user_model "code.gitea.io/gitea/models/user"
	"code.gitea.io/gitea/modules/base"
	"code.gitea.io/gitea/modules/markup"
	"code.gitea.io/gitea/modules/setting"
	"code.gitea.io/gitea/modules/svg"
	"code.gitea.io/gitea/modules/templates/eval"
	"code.gitea.io/gitea/modules/timeutil"
	"code.gitea.io/gitea/modules/util"
	"code.gitea.io/gitea/services/gitdiff"
	"code.gitea.io/gitea/services/webtheme"
)

// NewFuncMap returns functions for injecting to templates
func NewFuncMap() template.FuncMap {
	return map[string]any{
		"ctx": func() any { return nil }, // template context function

		"DumpVar": dumpVar,

		// -----------------------------------------------------------------
		// html/template related functions
		"dict":         dict, // it's lowercase because this name has been widely used. Our other functions should have uppercase names.
		"Iif":          iif,
		"Eval":         evalTokens,
		"SafeHTML":     safeHTML,
		"HTMLFormat":   HTMLFormat,
		"HTMLEscape":   htmlEscape,
		"QueryEscape":  queryEscape,
		"JSEscape":     jsEscapeSafe,
		"SanitizeHTML": SanitizeHTML,
		"URLJoin":      util.URLJoin,
		"DotEscape":    dotEscape,

		"PathEscape":         url.PathEscape,
		"PathEscapeSegments": util.PathEscapeSegments,

		// utils
		"StringUtils": NewStringUtils,
		"SliceUtils":  NewSliceUtils,
		"JsonUtils":   NewJsonUtils,

		// -----------------------------------------------------------------
		// svg / avatar / icon / color
		"svg":           svg.RenderHTML,
		"EntryIcon":     base.EntryIcon,
		"MigrationIcon": migrationIcon,
		"ActionIcon":    actionIcon,
		"SortArrow":     sortArrow,
		"ContrastColor": util.ContrastColor,

		// -----------------------------------------------------------------
		// time / number / format
		"FileSize":      base.FileSize,
		"CountFmt":      base.FormatNumberSI,
		"TimeSince":     timeutil.TimeSince,
		"TimeSinceUnix": timeutil.TimeSinceUnix,
		"DateTime":      timeutil.DateTime,
		"Sec2Time":      util.SecToTime,
		"LoadTimes": func(startTime time.Time) string {
			return fmt.Sprint(time.Since(startTime).Nanoseconds()/1e6) + "ms"
		},

		// -----------------------------------------------------------------
		// setting
		"AppName": func() string {
			return setting.AppName
		},
		"AppSubUrl": func() string {
			return setting.AppSubURL
		},
		"AssetUrlPrefix": func() string {
			return setting.StaticURLPrefix + "/assets"
		},
		"AppUrl": func() string {
			// The usage of AppUrl should be avoided as much as possible,
			// because the AppURL(ROOT_URL) may not match user's visiting site and the ROOT_URL in app.ini may be incorrect.
			// And it's difficult for Gitea to guess absolute URL correctly with zero configuration,
			// because Gitea doesn't know whether the scheme is HTTP or HTTPS unless the reverse proxy could tell Gitea.
			return setting.AppURL
		},
		"AppVer": func() string {
			return setting.AppVer
		},
		"AppDomain": func() string { // documented in mail-templates.md
			return setting.Domain
		},
		"AssetVersion": func() string {
			return setting.AssetVersion
		},
		"DefaultShowFullName": func() bool {
			return setting.UI.DefaultShowFullName
		},
		"ShowFooterTemplateLoadTime": func() bool {
			return setting.Other.ShowFooterTemplateLoadTime
		},
		"ShowFooterPoweredBy": func() bool {
			return setting.Other.ShowFooterPoweredBy
		},
		"AllowedReactions": func() []string {
			return setting.UI.Reactions
		},
		"CustomEmojis": func() map[string]string {
			return setting.UI.CustomEmojisMap
		},
		"MetaAuthor": func() string {
			return setting.UI.Meta.Author
		},
		"MetaDescription": func() string {
			return setting.UI.Meta.Description
		},
		"MetaKeywords": func() string {
			return setting.UI.Meta.Keywords
		},
		"EnableTimetracking": func() bool {
			return setting.Service.EnableTimetracking
		},
		"DisableGitHooks": func() bool {
			return setting.DisableGitHooks
		},
		"DisableWebhooks": func() bool {
			return setting.DisableWebhooks
		},
		"DisableImportLocal": func() bool {
			return !setting.ImportLocalPaths
		},
		"UserThemeName": userThemeName,
		"NotificationSettings": func() map[string]any {
			return map[string]any{
				"MinTimeout":            int(setting.UI.Notification.MinTimeout / time.Millisecond),
				"TimeoutStep":           int(setting.UI.Notification.TimeoutStep / time.Millisecond),
				"MaxTimeout":            int(setting.UI.Notification.MaxTimeout / time.Millisecond),
				"EventSourceUpdateTime": int(setting.UI.Notification.EventSourceUpdateTime / time.Millisecond),
			}
		},
		"MermaidMaxSourceCharacters": func() int {
			return setting.MermaidMaxSourceCharacters
		},

		// -----------------------------------------------------------------
		// render
		"RenderCommitMessage":            RenderCommitMessage,
		"RenderCommitMessageLinkSubject": renderCommitMessageLinkSubject,

		"RenderCommitBody": renderCommitBody,
		"RenderCodeBlock":  renderCodeBlock,
		"RenderIssueTitle": renderIssueTitle,
		"RenderEmoji":      renderEmoji,
		"ReactionToEmoji":  reactionToEmoji,

		"RenderMarkdownToHtml": RenderMarkdownToHtml,
		"RenderLabel":          renderLabel,
		"RenderLabels":         RenderLabels,

		// -----------------------------------------------------------------
		// misc
		"ShortSha":                 base.ShortSha,
		"ActionContent2Commits":    ActionContent2Commits,
		"IsMultilineCommitMessage": isMultilineCommitMessage,
		"CommentMustAsDiff":        gitdiff.CommentMustAsDiff,
		"MirrorRemoteAddress":      mirrorRemoteAddress,

		"FilenameIsImage": filenameIsImage,
		"TabSizeClass":    tabSizeClass,
	}
}

func HTMLFormat(s string, rawArgs ...any) template.HTML {
	args := slices.Clone(rawArgs)
	for i, v := range args {
		switch v := v.(type) {
		case nil, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, template.HTML:
			// for most basic types (including template.HTML which is safe), just do nothing and use it
		case string:
			args[i] = template.HTMLEscapeString(v)
		case fmt.Stringer:
			args[i] = template.HTMLEscapeString(v.String())
		default:
			args[i] = template.HTMLEscapeString(fmt.Sprint(v))
		}
	}
	return template.HTML(fmt.Sprintf(s, args...))
}

// safeHTML render raw as HTML
func safeHTML(s any) template.HTML {
	switch v := s.(type) {
	case string:
		return template.HTML(v)
	case template.HTML:
		return v
	}
	panic(fmt.Sprintf("unexpected type %T", s))
}

// SanitizeHTML sanitizes the input by pre-defined markdown rules
func SanitizeHTML(s string) template.HTML {
	return template.HTML(markup.Sanitize(s))
}

func htmlEscape(s any) template.HTML {
	switch v := s.(type) {
	case string:
		return template.HTML(html.EscapeString(v))
	case template.HTML:
		return v
	}
	panic(fmt.Sprintf("unexpected type %T", s))
}

func jsEscapeSafe(s string) template.HTML {
	return template.HTML(template.JSEscapeString(s))
}

func queryEscape(s string) template.URL {
	return template.URL(url.QueryEscape(s))
}

// dotEscape wraps a dots in names with ZWJ [U+200D] in order to prevent auto-linkers from detecting these as urls
func dotEscape(raw string) string {
	return strings.ReplaceAll(raw, ".", "\u200d.\u200d")
}

// iif is an "inline-if", similar util.Iif[T] but templates need the non-generic version,
// and it could be simply used as "{{iif expr trueVal}}" (omit the falseVal).
func iif(condition any, vals ...any) any {
	if isTemplateTruthy(condition) {
		return vals[0]
	} else if len(vals) > 1 {
		return vals[1]
	}
	return nil
}

func isTemplateTruthy(v any) bool {
	if v == nil {
		return false
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Bool:
		return rv.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int() != 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return rv.Uint() != 0
	case reflect.Float32, reflect.Float64:
		return rv.Float() != 0
	case reflect.Complex64, reflect.Complex128:
		return rv.Complex() != 0
	case reflect.String, reflect.Slice, reflect.Array, reflect.Map:
		return rv.Len() > 0
	case reflect.Struct:
		return true
	default:
		return !rv.IsNil()
	}
}

// evalTokens evaluates the expression by tokens and returns the result, see the comment of eval.Expr for details.
// To use this helper function in templates, pass each token as a separate parameter.
//
//	{{ $int64 := Eval $var "+" 1 }}
//	{{ $float64 := Eval $var "+" 1.0 }}
//
// Golang's template supports comparable int types, so the int64 result can be used in later statements like {{if lt $int64 10}}
func evalTokens(tokens ...any) (any, error) {
	n, err := eval.Expr(tokens...)
	return n.Value, err
}

func userThemeName(user *user_model.User) string {
	if user == nil || user.Theme == "" {
		return setting.UI.DefaultTheme
	}
	if webtheme.IsThemeAvailable(user.Theme) {
		return user.Theme
	}
	return setting.UI.DefaultTheme
}
