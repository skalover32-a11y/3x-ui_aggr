package backup

import "testing"

func TestMergeStorageConfigPreservesSecretsAndUpdatesFlags(t *testing.T) {
    existing := StorageTargetConfig{
        Host:               "old.example.com",
        Port:               22,
        Username:           "backup",
        Password:           "old-password",
        PrivateKeyPEM:      "old-key",
        SecretKey:          "old-secret",
        BasePath:           "/daily",
        PassiveMode:        true,
        InsecureSkipVerify: true,
        UseSSL:             true,
        PathStyle:          true,
    }

    merged := mergeStorageConfig(existing, StorageTargetConfig{
        Host:               "new.example.com",
        Password:           "",
        PrivateKeyPEM:      "",
        SecretKey:          "",
        BasePath:           "/weekly",
        PassiveMode:        false,
        InsecureSkipVerify: false,
        UseSSL:             false,
        PathStyle:          false,
    })

    if merged.Host != "new.example.com" {
        t.Fatalf("expected updated host, got %q", merged.Host)
    }
    if merged.BasePath != "/weekly" {
        t.Fatalf("expected updated base path, got %q", merged.BasePath)
    }
    if merged.Password != "old-password" {
        t.Fatal("expected password to be preserved when update is blank")
    }
    if merged.PrivateKeyPEM != "old-key" {
        t.Fatal("expected private key to be preserved when update is blank")
    }
    if merged.SecretKey != "old-secret" {
        t.Fatal("expected secret key to be preserved when update is blank")
    }
    if merged.PassiveMode {
        t.Fatal("expected passive mode flag to update to false")
    }
    if merged.InsecureSkipVerify {
        t.Fatal("expected insecure skip verify flag to update to false")
    }
    if merged.UseSSL {
        t.Fatal("expected use_ssl flag to update to false")
    }
    if merged.PathStyle {
        t.Fatal("expected path_style flag to update to false")
    }
}

func TestMaskStorageConfigHidesSecrets(t *testing.T) {
    masked := MaskStorageConfig(StorageTargetConfig{
        Host:          "s3.example.com",
        AccessKey:     "access",
        Password:      "password",
        PrivateKeyPEM: "private-key",
        SecretKey:     "secret",
    })

    if _, ok := masked["password"]; ok {
        t.Fatal("password should not be exposed in masked config")
    }
    if _, ok := masked["private_key_pem"]; ok {
        t.Fatal("private key should not be exposed in masked config")
    }
    if _, ok := masked["secret_key"]; ok {
        t.Fatal("secret key should not be exposed in masked config")
    }
    if masked["password_set"] != true {
        t.Fatal("expected password_set=true")
    }
    if masked["private_key_set"] != true {
        t.Fatal("expected private_key_set=true")
    }
    if masked["secret_key_set"] != true {
        t.Fatal("expected secret_key_set=true")
    }
}
