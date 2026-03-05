package mailer

import (
	"os"
	"strings"
	"testing"
)

// Mocked tests
func TestSendNewContractNotification_UsesSendMailFunc(t *testing.T) {
	orig := SendMailFunc
	defer func() { SendMailFunc = orig }()

	called := false
	var toGot, subjGot, bodyGot string
	SendMailFunc = func(to, subject, body string) error {
		called = true
		toGot, subjGot, bodyGot = to, subject, body
		return nil
	}

	if err := SendNewContractNotification("ops@example.com", "Anna Schmidt", "2025-10-01", "2025-09-24", "organic", "Abschluss", 190.0, "2025-11-01"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected SendMailFunc to be called")
	}
	if toGot != "ops@example.com" {
		t.Fatalf("unexpected to: %s", toGot)
	}
	if !strings.Contains(subjGot, "Anna Schmidt") {
		t.Fatalf("subject didn't contain client name: %s", subjGot)
	}
	if !strings.Contains(bodyGot, "190.00") {
		t.Fatalf("body didn't contain revenue: %s", bodyGot)
	}
	if !strings.Contains(bodyGot, "Startdatum: 2025-10-01") {
		t.Fatalf("body didn't contain start date: %s", bodyGot)
	}
	if !strings.Contains(bodyGot, "Abschlussdatum: 2025-09-24") {
		t.Fatalf("body didn't contain closure date: %s", bodyGot)
	}
	if !strings.Contains(bodyGot, "Quelle: organic") {
		t.Fatalf("body didn't contain source: %s", bodyGot)
	}
	if !strings.Contains(bodyGot, "Vertriebsphase: Abschluss") && !strings.Contains(bodyGot, "Bühne: Abschluss") {
		t.Fatalf("body didn't contain stage: %s", bodyGot)
	}
	if !strings.Contains(bodyGot, "Nächste Fälligkeit: 2025-11-01") {
		t.Fatalf("body didn't contain next due date: %s", bodyGot)
	}
}

// Real Implementation Tests
func TestSendMail_NoSMTP(t *testing.T) {
	// Ensure no SMTP is configured — SendMail should no-op and return nil
	os.Unsetenv("SMTP_HOST")
	if err := SendMail("test@example.com", "sub", "body"); err != nil {
		t.Fatalf("expected nil error when SMTP_HOST unset, got: %v", err)
	}
}

func TestSendNewContractNotification_NoSMTP(t *testing.T) {
	os.Unsetenv("SMTP_HOST")
	if err := SendNewContractNotification("ops@example.com", "Max Müller", "2025-10-01", "", "", "", 123.45, ""); err != nil {
		t.Fatalf("expected nil error when SMTP_HOST unset, got: %v", err)
	}
}
