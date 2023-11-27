// Copyright 2023 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package graceful

import (
	"context"
	"runtime/pprof"
	"sync"
	"time"
)

type systemdNotifyMsg string

const (
	readyMsg     systemdNotifyMsg = "READY=1"
	stoppingMsg  systemdNotifyMsg = "STOPPING=1"
	reloadingMsg systemdNotifyMsg = "RELOADING=1"
	watchdogMsg  systemdNotifyMsg = "WATCHDOG=1"
)

func statusMsg(msg string) systemdNotifyMsg {
	return systemdNotifyMsg("STATUS=" + msg)
}

// Manager manages the graceful shutdown process
type Manager struct {
	ctx                    context.Context
	isChild                bool
	forked                 bool
	lock                   sync.RWMutex
	state                  state
	shutdownCtx            context.Context
	hammerCtx              context.Context
	terminateCtx           context.Context
	managerCtx             context.Context
	shutdownCtxCancel      context.CancelFunc
	hammerCtxCancel        context.CancelFunc
	terminateCtxCancel     context.CancelFunc
	managerCtxCancel       context.CancelFunc
	runningServerWaitGroup sync.WaitGroup
	createServerWaitGroup  sync.WaitGroup
	terminateWaitGroup     sync.WaitGroup
	shutdownRequested      chan struct{}

	toRunAtShutdown  []func()
	toRunAtTerminate []func()
}

func newGracefulManager(ctx context.Context) *Manager {
	manager := &Manager{ctx: ctx, shutdownRequested: make(chan struct{})}
	manager.createServerWaitGroup.Add(numberOfServersToCreate)
	manager.prepare(ctx)
	manager.start()
	return manager
}

func (g *Manager) prepare(ctx context.Context) {
	g.terminateCtx, g.terminateCtxCancel = context.WithCancel(ctx)
	g.shutdownCtx, g.shutdownCtxCancel = context.WithCancel(ctx)
	g.hammerCtx, g.hammerCtxCancel = context.WithCancel(ctx)
	g.managerCtx, g.managerCtxCancel = context.WithCancel(ctx)

	g.terminateCtx = pprof.WithLabels(g.terminateCtx, pprof.Labels("graceful-lifecycle", "with-terminate"))
	g.shutdownCtx = pprof.WithLabels(g.shutdownCtx, pprof.Labels("graceful-lifecycle", "with-shutdown"))
	g.hammerCtx = pprof.WithLabels(g.hammerCtx, pprof.Labels("graceful-lifecycle", "with-hammer"))
	g.managerCtx = pprof.WithLabels(g.managerCtx, pprof.Labels("graceful-lifecycle", "with-manager"))

	if !g.setStateTransition(stateInit, stateRunning) {
		panic("invalid graceful manager state: transition from init to running failed")
	}
}

// DoImmediateHammer causes an immediate hammer
func (g *Manager) DoImmediateHammer() {
	g.notify(statusMsg("Sending immediate hammer"))
	g.doHammerTime(0 * time.Second)
}

// DoGracefulShutdown causes a graceful shutdown
func (g *Manager) DoGracefulShutdown() {
	g.lock.Lock()
	select {
	case <-g.shutdownRequested:
	default:
		close(g.shutdownRequested)
	}
	forked := g.forked
	g.lock.Unlock()

	if !forked {
		g.notify(stoppingMsg)
	} else {
		g.notify(statusMsg("Shutting down after fork"))
	}
	g.doShutdown()
}

// RegisterServer registers the running of a listening server, in the case of unix this means that the parent process can now die.
// Any call to RegisterServer must be matched by a call to ServerDone
func (g *Manager) RegisterServer() {
	KillParent()
	g.runningServerWaitGroup.Add(1)
}
