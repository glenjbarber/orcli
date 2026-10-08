package config

import "testing"

func TestApiarySettings(t *testing.T) {
	h := home(t)
	writeConfigAt(t, h, `{"api_key":"model-key","apiary":{"base_url":"https://apiary.example","viewer_token":"apk_viewer","future":"kept"}}`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	base, token, err := cfg.ApiarySettings()
	if err != nil || base != "https://apiary.example" || token != "apk_viewer" {
		t.Fatalf("settings = %q %q, %v", base, token, err)
	}
}

func TestApiarySettingsRejectsWrongTypes(t *testing.T) {
	h := home(t)
	writeConfigAt(t, h, `{"api_key":"model-key","apiary":{"viewer_token":42}}`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = cfg.ApiarySettings(); err == nil {
		t.Fatal("accepted non-string Viewer token")
	}
}
