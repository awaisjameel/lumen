package services

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"lumen/internal/core"
	"lumen/internal/update"
)

// EventUpdateStatus carries ordered installation snapshots across remounts.
const EventUpdateStatus = "update:status"

func init() { application.RegisterEvent[core.UpdateStatus](EventUpdateStatus) }

// UpdateService is the typed UI boundary; the coordinator owns operation lifetimes.
type UpdateService struct {
	manager *update.Manager
	dataDir string
	health  func(context.Context) error
}

// NewUpdateService shares the installation coordinator with the application lifecycle.
func NewUpdateService(manager *update.Manager, dataDir string, health func(context.Context) error) *UpdateService {
	return &UpdateService{manager: manager, dataDir: dataDir, health: health}
}

// ServiceStartup starts check-only discovery.
func (s *UpdateService) ServiceStartup(_ context.Context, _ application.ServiceOptions) error {
	s.manager.Start()
	return nil
}

// ServiceShutdown joins the requested preparation before shutdown proceeds.
func (s *UpdateService) ServiceShutdown() error { return s.manager.Close() }

// Status exposes the current installation snapshot.
func (s *UpdateService) Status() core.UpdateStatus { return s.manager.Status() }

// SetPolicy persists installation-scoped policy in save order.
func (s *UpdateService) SetPolicy(ctx context.Context, p core.UpdatePolicy) error {
	return s.manager.SetPolicy(ctx, p)
}

// Check requests discovery only, including when automatic checks are disabled.
func (s *UpdateService) Check(ctx context.Context) error { return s.manager.Check(ctx) }

// Download starts only an explicitly requested background download.
func (s *UpdateService) Download() error { return s.manager.Download() }

// Cancel stops discovery or preparation without changing the installed version.
func (s *UpdateService) Cancel() error { return s.manager.Cancel() }

// Discard removes the prepared candidate only.
func (s *UpdateService) Discard(ctx context.Context) error { return s.manager.Discard(ctx) }

// Dismiss keeps a per-release availability dismissal outside the photo library.
func (s *UpdateService) Dismiss(ctx context.Context) error { return s.manager.Dismiss(ctx) }

// Restart requires a ready candidate and a freshly authenticated release.
func (s *UpdateService) Restart(ctx context.Context) error {
	if err := s.manager.RequestRestart(ctx, s.dataDir); err != nil {
		return err
	}
	application.Get().Quit()
	return nil
}

// Health is called by the embedded frontend before normal library initialisation.
func (s *UpdateService) Health(ctx context.Context) error {
	if s.health != nil {
		return s.health(ctx)
	}
	return nil
}
