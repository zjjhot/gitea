// Copyright 2020 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package user

import (
	"net/http"
	"strconv"

	"code.gitea.io/gitea/models"
	"code.gitea.io/gitea/modules/context"
	"code.gitea.io/gitea/modules/json"
)

// TaskStatus returns task's status
func TaskStatus(ctx *context.Context) {
	task, opts, err := models.GetMigratingTaskByID(ctx.ParamsInt64("task"), ctx.Doer.ID)
	if err != nil {
		if models.IsErrTaskDoesNotExist(err) {
			ctx.JSON(http.StatusNotFound, map[string]interface{}{
				"error": "task `" + strconv.FormatInt(ctx.ParamsInt64("task"), 10) + "` does not exist",
			})
			return
		}
		ctx.JSON(http.StatusInternalServerError, map[string]interface{}{
			"err": err,
		})
		return
	}

	message := task.Message

	if task.Message != "" && task.Message[0] == '{' {
		// assume message is actually a translatable string
		var translatableMessage models.TranslatableMessage
		if err := json.Unmarshal([]byte(message), &translatableMessage); err != nil {
			translatableMessage = models.TranslatableMessage{
				Format: "migrate.migrating_failed.error",
				Args:   []interface{}{task.Message},
			}
		}
		message = ctx.Tr(translatableMessage.Format, translatableMessage.Args...)
	}

	ctx.JSON(http.StatusOK, map[string]interface{}{
		"status":    task.Status,
		"message":   message,
		"repo-id":   task.RepoID,
		"repo-name": opts.RepoName,
		"start":     task.StartTime,
		"end":       task.EndTime,
	})
}
