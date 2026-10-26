// Reviewer case: mail out.
// want: import of net/smtp is not on the allowlist
package fixture

import "net/smtp"

func f() error {
	return smtp.SendMail("mail.example.com:25", nil, "a@example.com", []string{"b@example.com"}, []byte("x"))
}
