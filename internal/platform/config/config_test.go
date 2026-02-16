package config

import (
	"os"
	"testing"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		setup   func()
		cleanup func()
		want    Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "all required env vars set",
			setup: func() {
				_ = os.Setenv("WEBHOOK_SECRET", "test-secret")
				_ = os.Setenv("GITHUB_APP_ID", "123456")
				_ = os.Setenv("GITHUB_INSTALLATION_ID", "789012")
				_ = os.Setenv("GITHUB_PRIVATE_KEY", "test-key")
				_ = os.Setenv("PORT", "9000")
				_ = os.Setenv("LOG_LEVEL", "debug")
			},
			cleanup: func() {
				_ = os.Unsetenv("WEBHOOK_SECRET")
				_ = os.Unsetenv("GITHUB_APP_ID")
				_ = os.Unsetenv("GITHUB_INSTALLATION_ID")
				_ = os.Unsetenv("GITHUB_PRIVATE_KEY")
				_ = os.Unsetenv("PORT")
				_ = os.Unsetenv("LOG_LEVEL")
			},
			want: Config{
				Port:                 9000,
				WebhookSecret:        "test-secret",
				GitHubAppID:          123456,
				GitHubInstallationID: 789012,
				GitHubPrivateKey:     "test-key",
				LogLevel:             "debug",
			},
			wantErr: false,
		},
		{
			name: "defaults for optional vars",
			setup: func() {
				_ = os.Setenv("WEBHOOK_SECRET", "test-secret")
				_ = os.Setenv("GITHUB_APP_ID", "123456")
				_ = os.Setenv("GITHUB_INSTALLATION_ID", "789012")
				_ = os.Setenv("GITHUB_PRIVATE_KEY", "test-key")
				// PORT and LOG_LEVEL not set
			},
			cleanup: func() {
				_ = os.Unsetenv("WEBHOOK_SECRET")
				_ = os.Unsetenv("GITHUB_APP_ID")
				_ = os.Unsetenv("GITHUB_INSTALLATION_ID")
				_ = os.Unsetenv("GITHUB_PRIVATE_KEY")
			},
			want: Config{
				Port:                 8080, // Default
				WebhookSecret:        "test-secret",
				GitHubAppID:          123456,
				GitHubInstallationID: 789012,
				GitHubPrivateKey:     "test-key",
				LogLevel:             "info", // Default
			},
			wantErr: false,
		},
		{
			name: "missing WEBHOOK_SECRET",
			setup: func() {
				_ = os.Setenv("GITHUB_APP_ID", "123456")
				_ = os.Setenv("GITHUB_INSTALLATION_ID", "789012")
				_ = os.Setenv("GITHUB_PRIVATE_KEY", "test-key")
			},
			cleanup: func() {
				_ = os.Unsetenv("GITHUB_APP_ID")
				_ = os.Unsetenv("GITHUB_INSTALLATION_ID")
				_ = os.Unsetenv("GITHUB_PRIVATE_KEY")
			},
			wantErr: true,
			errMsg:  "WEBHOOK_SECRET",
		},
		{
			name: "missing GITHUB_APP_ID",
			setup: func() {
				_ = os.Setenv("WEBHOOK_SECRET", "test-secret")
				_ = os.Setenv("GITHUB_INSTALLATION_ID", "789012")
				_ = os.Setenv("GITHUB_PRIVATE_KEY", "test-key")
			},
			cleanup: func() {
				_ = os.Unsetenv("WEBHOOK_SECRET")
				_ = os.Unsetenv("GITHUB_INSTALLATION_ID")
				_ = os.Unsetenv("GITHUB_PRIVATE_KEY")
			},
			wantErr: true,
			errMsg:  "GITHUB_APP_ID",
		},
		{
			name: "missing GITHUB_INSTALLATION_ID",
			setup: func() {
				_ = os.Setenv("WEBHOOK_SECRET", "test-secret")
				_ = os.Setenv("GITHUB_APP_ID", "123456")
				_ = os.Setenv("GITHUB_PRIVATE_KEY", "test-key")
			},
			cleanup: func() {
				_ = os.Unsetenv("WEBHOOK_SECRET")
				_ = os.Unsetenv("GITHUB_APP_ID")
				_ = os.Unsetenv("GITHUB_PRIVATE_KEY")
			},
			wantErr: true,
			errMsg:  "GITHUB_INSTALLATION_ID",
		},
		{
			name: "missing GITHUB_PRIVATE_KEY",
			setup: func() {
				_ = os.Setenv("WEBHOOK_SECRET", "test-secret")
				_ = os.Setenv("GITHUB_APP_ID", "123456")
				_ = os.Setenv("GITHUB_INSTALLATION_ID", "789012")
			},
			cleanup: func() {
				_ = os.Unsetenv("WEBHOOK_SECRET")
				_ = os.Unsetenv("GITHUB_APP_ID")
				_ = os.Unsetenv("GITHUB_INSTALLATION_ID")
			},
			wantErr: true,
			errMsg:  "GITHUB_PRIVATE_KEY",
		},
		{
			name: "invalid PORT",
			setup: func() {
				_ = os.Setenv("WEBHOOK_SECRET", "test-secret")
				_ = os.Setenv("GITHUB_APP_ID", "123456")
				_ = os.Setenv("GITHUB_INSTALLATION_ID", "789012")
				_ = os.Setenv("GITHUB_PRIVATE_KEY", "test-key")
				_ = os.Setenv("PORT", "not-a-number")
			},
			cleanup: func() {
				_ = os.Unsetenv("WEBHOOK_SECRET")
				_ = os.Unsetenv("GITHUB_APP_ID")
				_ = os.Unsetenv("GITHUB_INSTALLATION_ID")
				_ = os.Unsetenv("GITHUB_PRIVATE_KEY")
				_ = os.Unsetenv("PORT")
			},
			wantErr: true,
			errMsg:  "PORT",
		},
		{
			name: "invalid GITHUB_APP_ID",
			setup: func() {
				_ = os.Setenv("WEBHOOK_SECRET", "test-secret")
				_ = os.Setenv("GITHUB_APP_ID", "not-a-number")
				_ = os.Setenv("GITHUB_INSTALLATION_ID", "789012")
				_ = os.Setenv("GITHUB_PRIVATE_KEY", "test-key")
			},
			cleanup: func() {
				_ = os.Unsetenv("WEBHOOK_SECRET")
				_ = os.Unsetenv("GITHUB_APP_ID")
				_ = os.Unsetenv("GITHUB_INSTALLATION_ID")
				_ = os.Unsetenv("GITHUB_PRIVATE_KEY")
			},
			wantErr: true,
			errMsg:  "GITHUB_APP_ID",
		},
		{
			name: "invalid GITHUB_INSTALLATION_ID",
			setup: func() {
				_ = os.Setenv("WEBHOOK_SECRET", "test-secret")
				_ = os.Setenv("GITHUB_APP_ID", "123456")
				_ = os.Setenv("GITHUB_INSTALLATION_ID", "not-a-number")
				_ = os.Setenv("GITHUB_PRIVATE_KEY", "test-key")
			},
			cleanup: func() {
				_ = os.Unsetenv("WEBHOOK_SECRET")
				_ = os.Unsetenv("GITHUB_APP_ID")
				_ = os.Unsetenv("GITHUB_INSTALLATION_ID")
				_ = os.Unsetenv("GITHUB_PRIVATE_KEY")
			},
			wantErr: true,
			errMsg:  "GITHUB_INSTALLATION_ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup()
			defer tt.cleanup()

			got, err := Load()

			if tt.wantErr {
				if err == nil {
					t.Errorf("Load() expected error containing %q, got nil", tt.errMsg)
					return
				}
				if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("Load() error = %v, want error containing %q", err, tt.errMsg)
				}
				return
			}

			if err != nil {
				t.Errorf("Load() unexpected error = %v", err)
				return
			}

			if got.Port != tt.want.Port {
				t.Errorf("Load().Port = %v, want %v", got.Port, tt.want.Port)
			}
			if got.WebhookSecret != tt.want.WebhookSecret {
				t.Errorf(
					"Load().WebhookSecret = %v, want %v",
					got.WebhookSecret,
					tt.want.WebhookSecret,
				)
			}
			if got.GitHubAppID != tt.want.GitHubAppID {
				t.Errorf("Load().GitHubAppID = %v, want %v", got.GitHubAppID, tt.want.GitHubAppID)
			}
			if got.GitHubInstallationID != tt.want.GitHubInstallationID {
				t.Errorf(
					"Load().GitHubInstallationID = %v, want %v",
					got.GitHubInstallationID,
					tt.want.GitHubInstallationID,
				)
			}
			if got.GitHubPrivateKey != tt.want.GitHubPrivateKey {
				t.Errorf(
					"Load().GitHubPrivateKey = %v, want %v",
					got.GitHubPrivateKey,
					tt.want.GitHubPrivateKey,
				)
			}
			if got.LogLevel != tt.want.LogLevel {
				t.Errorf("Load().LogLevel = %v, want %v", got.LogLevel, tt.want.LogLevel)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 &&
		(s == substr || len(s) >= len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsInner(s, substr)))
}

func containsInner(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
