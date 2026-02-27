package mailer

import (
    "strings"
    "testing"
)

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

    if err := SendNewContractNotification("ops@example.com", 5, 7, 190.0, "2025-10-01"); err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if !called {
        t.Fatal("expected SendMailFunc to be called")
    }
    if toGot != "ops@example.com" {
        t.Fatalf("unexpected to: %s", toGot)
    }
    if !strings.Contains(subjGot, "#5") {
        t.Fatalf("subject didn't contain contract id: %s", subjGot)
    }
    if !strings.Contains(bodyGot, "190.00") {
        t.Fatalf("body didn't contain revenue: %s", bodyGot)
    }
}
