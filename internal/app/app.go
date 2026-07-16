package app

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"time"

	"github.com/antopolskiy/vault-rename/internal/apperr"
	"github.com/antopolskiy/vault-rename/internal/audit"
	"github.com/antopolskiy/vault-rename/internal/config"
	"github.com/antopolskiy/vault-rename/internal/model"
	"github.com/antopolskiy/vault-rename/internal/planner"
	"github.com/antopolskiy/vault-rename/internal/state"
	"github.com/antopolskiy/vault-rename/internal/transaction"
	"github.com/antopolskiy/vault-rename/internal/vaultlock"
)

func Run(ctx context.Context, request model.Request, version string) (model.Result, error) {
	root, err := filepath.Abs(request.Root)
	if err != nil {
		return model.Result{}, apperr.Wrap(apperr.CodeIOError, "cannot resolve vault root", err)
	}
	cfg, _, err := config.Load(root, request.ConfigPath)
	if err != nil {
		return model.Result{}, err
	}
	backlinks := cfg.Backlinks
	if request.BacklinksOverride != "" {
		backlinks = request.BacklinksOverride
		cfg.Backlinks = backlinks
		if err := cfg.Validate(); err != nil {
			return model.Result{}, err
		}
	}
	paths, err := state.Resolve(root, cfg)
	if err != nil {
		return model.Result{}, err
	}
	if request.DryRun {
		pending, pendingErr := transaction.Pending(paths)
		if pendingErr != nil {
			return model.Result{}, pendingErr
		}
		if pending {
			return model.Result{}, apperr.New(apperr.CodeRecoveryConflict, "an unfinished operation requires recovery")
		}
		plan, planErr := planner.Build(root, request.Source, request.NewName, cfg, backlinks, paths.Base, paths.Recovery)
		if planErr != nil {
			return model.Result{}, planErr
		}
		return model.Result{
			Status:       "planned",
			Source:       plan.Source,
			Destination:  plan.Destination,
			FilesChanged: affectedCount(plan),
			LinksUpdated: plan.LinksUpdated,
			Warnings:     plan.Warnings,
		}, nil
	}
	if request.Reason == "" {
		return model.Result{}, apperr.New(apperr.CodeConfigError, "--reason is required for a mutating rename")
	}
	if request.Actor == "" {
		request.Actor = defaultActor()
	}
	if err := state.Ensure(paths); err != nil {
		return model.Result{}, err
	}
	lock, err := vaultlock.Acquire(paths.Lock)
	if err != nil {
		return model.Result{}, err
	}
	defer lock.Close()

	store, err := audit.Open(paths.Log)
	if err != nil {
		return model.Result{}, err
	}
	defer store.Close()
	if err := transaction.RecoverPending(ctx, paths, store); err != nil {
		return model.Result{}, err
	}
	plan, err := planner.Build(root, request.Source, request.NewName, cfg, backlinks, paths.Base, paths.Recovery)
	if err != nil {
		return model.Result{}, err
	}
	operationID, err := newUUID()
	if err != nil {
		return model.Result{}, err
	}
	return transaction.Execute(
		ctx, paths, store, paths.VaultID, plan,
		model.AuditContext{
			OperationID:   operationID,
			Actor:         request.Actor,
			Reason:        request.Reason,
			BatchID:       request.BatchID,
			ToolVersion:   version,
			ConfigVersion: cfg.Version,
			StartedAt:     time.Now().UTC(),
		},
		transaction.TestFailpointFromEnvironment(),
	)
}

func newUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", apperr.Wrap(apperr.CodeIOError, "cannot generate operation id", err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}

func defaultActor() string {
	if current, err := user.Current(); err == nil && current.Username != "" {
		return current.Username
	}
	if value := os.Getenv("USER"); value != "" {
		return value
	}
	if value := os.Getenv("USERNAME"); value != "" {
		return value
	}
	return "unknown"
}

func affectedCount(plan model.Plan) int {
	for _, change := range plan.FileChanges {
		if change.Path == plan.Source {
			return len(plan.FileChanges)
		}
	}
	return len(plan.FileChanges) + 1
}
