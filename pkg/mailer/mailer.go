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

// SendNewContractNotification composes a German notification for new contracts.
func SendNewContractNotification(to, clientName, startDate, closureDate, stageName string, revenue float64, nextDueDate string) error {
	subject := fmt.Sprintf("Neuer Vertrag bestätigt: %s", clientName)
	body := fmt.Sprintf("Ein neuer Vertrag wurde angelegt.\n\nKunde: %s\nStartdatum: %s\n", clientName, startDate)
	if closureDate != "" {
		body += fmt.Sprintf("Abschlussdatum: %s\n", closureDate)
	}
	if stageName != "" {
		body += fmt.Sprintf("Stage: %s\n", stageName)
	}
	body += fmt.Sprintf("Umsatz: %.2f\n", revenue)
	if nextDueDate != "" {
		body += fmt.Sprintf("Nächste Fälligkeit: %s\n", nextDueDate)
	}
	return SendMailFunc(to, subject, body)
}

// SendMailFunc is a package-level variable used to send mail. Tests may replace
// this with a stub to avoid sending real email and to assert parameters.
var SendMailFunc = SendMail
