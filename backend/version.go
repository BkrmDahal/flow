package backend

// AppVersion is the single source of truth for the current application version.
// Update this when bumping a release (also update CHANGELOG.md and scripts/release.sh).
const AppVersion = "0.8.3"

// GetAppVersion returns the current application version to the frontend.
func (a *App) GetAppVersion() string {
	return AppVersion
}
