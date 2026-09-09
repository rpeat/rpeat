package rpeat

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"net/smtp"
	"os"
	"strings"
)

// https://stackoverflow.com/questions/58804817/setting-up-standard-go-net-smtp-with-office-365-fails-with-error-tls-first-rec
type loginAuth struct {
	username, password string
}

func LoginAuth(username, password string) smtp.Auth {
	return &loginAuth{username, password}
}

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	return "LOGIN", []byte{}, nil
}

func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if more {
		switch string(fromServer) {
		case "Username:":
			return []byte(a.username), nil
		case "Password:":
			return []byte(a.password), nil
		default:
			return nil, errors.New("Unkown fromServer")
		}
	}
	return nil, nil
}

func gmailAlert(alert AlertParams) error {
	alert.Endpoint = "smtp.gmail.com:587"
	return smtpAlert(alert)
}
func office365Alert(alert AlertParams) error {
	alert.Endpoint = "smtp.office365.com:587"
	return smtpAlert(alert)
}

func smtpAlert(alert AlertParams) error {
	smtp_credentials, ok := os.LookupEnv("RPEAT_SMTP")
	if !ok {
		ServerLogger.Println("credentials not found in RPEAT_SMTP environment variable")
		return errors.New("failed to locate RPEAT_SMTP Environment Variable")
	}
	user_pw := strings.Split(smtp_credentials, ";")
	auth := LoginAuth(user_pw[0], user_pw[1])

	From := fmt.Sprintf("From: rpeat-%s <%s>\r\n", alert.ServerName, user_pw[0])
	if alert.Alert.From != nil {
		From = fmt.Sprintf("From: %s <%s>\r\n", *alert.Alert.From, user_pw[0])
	}

	var to, To []string
	if alert.Alert.To != nil {
		for _, email := range alert.Alert.To {
			to = append(to, *email)
			To = append(To, "To: "+(*email)+"\r\n")
		}
	}

	var Cc []string
	if alert.Alert.CC != nil {
		for _, email := range alert.Alert.CC {
			to = append(to, *email)
			Cc = append(Cc, "Cc: "+(*email)+"\r\n")
		}
	}

	if alert.Alert.BCC != nil {
		for _, email := range alert.Alert.BCC {
			to = append(to, *email)
		}
	}

	subject := fmt.Sprintf("%s: %s", alert.Name, alert.JobStateString)
	if alert.Alert.Subject != nil {
		subject = *alert.Alert.Subject
	}
	subject = "Subject: " + subject + "\r\n"
	mime := "MIME-version: 1.0;\nContent-Type: text/html; charset=\"UTF-8\";\r\n\r\n"
	message := DefaultAlertMessageHTML
	if alert.Alert.Message != nil {
		message = *alert.Alert.Message + "<br><br><img src='https://rpeat.io/assets/img/poweredbyrpeat.png'/>"
	}

	var msgBuf bytes.Buffer
	tmpl, err := template.New("Msg").Parse(message)
	if err != nil {
		tmpl, _ = template.New("Msg").Parse(DefaultAlertMessageHTML)
	}
	tmpl.Execute(&msgBuf, alert)

	msg := []byte(From + strings.Join(To, "") + strings.Join(Cc, "") + subject + mime + msgBuf.String())

	err = smtp.SendMail(alert.Endpoint, auth, user_pw[0], to, msg)
	if err != nil {
		ConnectionLogger.Println(err)
	}
	return err
}
