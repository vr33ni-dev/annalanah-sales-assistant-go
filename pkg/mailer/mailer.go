package mailer

import (
	"fmt"
	"net/smtp"
	"os"
	"strings"
)

// SendMail sends a simple email using SMTP configuration from env:
// SMTP_HOST, SMTP_PORT, SMTP_USER, SMTP_PASS, SMTP_FROM
func SendMail(to, subject, body string) error {
	host := os.Getenv("SMTP_HOST")
	if host == "" {
		// no SMTP configured — skip sending
		return nil
	}
	port := os.Getenv("SMTP_PORT")
	if port == "" {
		port = "25"
	}
	from := os.Getenv("SMTP_FROM")
	if from == "" {
		from = "no-reply@localhost"
	}

	msg := []string{
		fmt.Sprintf("From: %s", from),
		fmt.Sprintf("To: %s", to),
		fmt.Sprintf("Subject: %s", subject),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"",
		body,
	}
	payload := strings.Join(msg, "\r\n")

	addr := host + ":" + port

	user := os.Getenv("SMTP_USER")
	pass := os.Getenv("SMTP_PASS")
	var auth smtp.Auth
	if user != "" && pass != "" {
		auth = smtp.PlainAuth("", user, pass, host)
	}

	return smtp.SendMail(addr, auth, from, []string{to}, []byte(payload))
}

// SendNewContractNotification composes a basic message for new contracts.
func SendNewContractNotification(to string, contractID int, clientID int, revenue float64, startDate string) error {
	subject := fmt.Sprintf("New contract confirmed: #%d", contractID)
	body := fmt.Sprintf("A new contract was created.\n\nContract ID: %d\nClient ID: %d\nStart Date: %s\nRevenue: %.2f\n", contractID, clientID, startDate, revenue)
	return SendMailFunc(to, subject, body)
}

// SendMailFunc is a package-level variable used to send mail. Tests may replace
// this with a stub to avoid sending real email and to assert parameters.
var SendMailFunc = SendMail
