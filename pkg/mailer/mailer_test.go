package mailer

import (
    "os"
    "testing"
)

func TestSendMail_NoSMTP(t *testing.T) {
    // Ensure no SMTP is configured — SendMail should no-op and return nil
    os.Unsetenv("SMTP_HOST")
    if err := SendMail("test@example.com", "sub", "body"); err != nil {
        t.Fatalf("expected nil error when SMTP_HOST unset, got: %v", err)
    }
}

func TestSendNewContractNotification_NoSMTP(t *testing.T) {
    os.Unsetenv("SMTP_HOST")
    if err := SendNewContractNotification("ops@example.com", 1, 2, 123.45, "2025-10-01"); err != nil {
        t.Fatalf("expected nil error when SMTP_HOST unset, got: %v", err)
    }
}
