package main

import (
	"fmt"
	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"log"
	"strings"
	"time"
)

//EmailBoxHandler used for handling email checks and sending notifications to user
type EmailBoxHandler struct {
	eAccount           *StoredEmailAccount
	user               *StoredUser
	lastMsgId          uint32
	lastMsgTime        int64
	isRestart          bool
	connectionOk       bool
	imapRetriesCounter int
	authRetriesCounter int
	stop               chan struct{}
}

func NewEmailBoxHandler(eAccount *StoredEmailAccount, user *StoredUser) *EmailBoxHandler {
	return &EmailBoxHandler{
		eAccount:    eAccount,
		user:        user,
		lastMsgId:   0,
		lastMsgTime: time.Now().Unix(),
		stop:        make(chan struct{}, 1),
	}
}

func (handler *EmailBoxHandler) StartFetchingEmails() {
	errMsg := ""
	handler.FetchNewEmails()
	if !handler.connectionOk {
		handler.eAccount.isActive = false
		return
	}
	ticker := time.NewTicker(time.Duration(handler.eAccount.updateT) * time.Minute)
MSGGETTINGLOOP:
	for {
		select {
		case <-handler.stop:
			if !handler.isRestart {
				errMsg = "Stopped fetching emails for " + handler.eAccount.login
			} else {
				handler.isRestart = false
			}
			break MSGGETTINGLOOP
		case <-ticker.C:
			if !handler.eAccount.isActive {
				handler.stop <- struct{}{}
			} else {
				handler.FetchNewEmails()
			}
		}
	}
	ticker.Stop()

	log.Println("Stopped worker", handler)
	if errMsg != "" {
		handler.SendMessageToUser(errMsg)
	}
}

func (handler *EmailBoxHandler) FetchNewEmails() {
	// Connect to server
	c, err := client.DialTLS(handler.eAccount.imapHost, nil)
	errMsg := ""
	if err != nil {
		log.Printf("Error connecting to imap server %s. %v", handler.eAccount.imapHost, err)
		handler.imapRetriesCounter += 1
		if handler.imapRetriesCounter == 3 {
			handler.eAccount.isActive = false
			errMsg = fmt.Sprintf("Error connecting to imap server: %s after %d retries",
				handler.eAccount.imapHost,
				handler.imapRetriesCounter)
			handler.SendMessageToUser(errMsg)
			handler.connectionOk = false
		}

		return
	}
	handler.imapRetriesCounter = 0

	// Don't forget to logout
	defer c.Logout()

	// Login
	if err := c.Login(handler.eAccount.login, handler.eAccount.password); err != nil {
		log.Printf("Error authenticating in account %s. %v", handler.eAccount.login, err)
		handler.authRetriesCounter += 1
		if handler.authRetriesCounter == 3 {
			handler.eAccount.isActive = false
			errMsg = fmt.Sprintf("Error authenticating in account: %s after %d retries",
				handler.eAccount.login,
				handler.authRetriesCounter)
			handler.connectionOk = false
			handler.SendMessageToUser(errMsg)
		}

		return
	}
	handler.authRetriesCounter = 0

	if !handler.connectionOk {
		handler.connectionOk = true
		uMsg := fmt.Sprintf("Successfully connected to mailbox for %s", handler.eAccount.login)
		handler.SendMessageToUser(uMsg)
	}

	// Select INBOX
	mbox, err := c.Select("INBOX", false)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Fetching emails")
	//always get last 20 messages and compare time with of last known
	from := mbox.Messages - 19
	to := mbox.Messages
	seqset := new(imap.SeqSet)
	seqset.AddRange(from, to)

	messages := make(chan *imap.Message, 100)
	done := make(chan error, 1)
	go func() {
		done <- c.Fetch(seqset, []imap.FetchItem{imap.FetchEnvelope}, messages)
	}()

	for msg := range messages {
		msgTime := msg.Envelope.Date.Unix()
		if msgTime <= handler.lastMsgTime {
			continue
		}
		handler.lastMsgTime = msgTime
		if handler.CheckPatterns(msg) {
			newUserMsg := fmt.Sprintf("At: %s\n", msg.Envelope.Date.Format("2006-01-02 15:04:05"))
			newUserMsg += fmt.Sprintf("Account: %s\n", handler.eAccount.login)
			newUserMsg += fmt.Sprintf("From: %s\n", msg.Envelope.From[0].PersonalName)
			newUserMsg += fmt.Sprintf("Subject: %s\n", msg.Envelope.Subject)
			handler.SendMessageToUser(newUserMsg)
		}
	}

	if err := <-done; err != nil {
		newUserMsg := "Error getting emails: " + err.Error()
		handler.SendMessageToUser(newUserMsg)
	}

	handler.lastMsgId = mbox.Messages
}

func (handler *EmailBoxHandler) CheckPatterns(msg *imap.Message) bool {
	sendEmail := false
	if len(handler.user.Patterns) == 0 {
		return true
	}
	msgSubj := strings.ToLower(msg.Envelope.Subject)
	for _, uPattern := range handler.user.Patterns {
		if uPattern.Subject != "" {
			if strings.Contains(msgSubj, uPattern.Subject) {
				sendEmail = true
				break
			}
		}
		if uPattern.FromEmail != "" || uPattern.FromPersonalName != "" {
			for _, sAddr := range msg.Envelope.From {
				msgMaiBoxes := strings.ToLower(sAddr.MailboxName)
				msgPerson := strings.ToLower(sAddr.PersonalName)
				if msgMaiBoxes == uPattern.FromEmail {
					sendEmail = true
					break
				}
				if msgPerson == uPattern.FromPersonalName {
					sendEmail = true
					break
				}
			}
			if sendEmail {
				break
			}
		}
	}
	return sendEmail
}

func (handler *EmailBoxHandler) Restart() {
	handler.isRestart = true
	handler.stop <- struct{}{}
	handler.eAccount.isActive = true
	go handler.StartFetchingEmails()
}

func (handler *EmailBoxHandler) Stop() {
	handler.eAccount.isActive = false
	handler.stop <- struct{}{}
}

func (handler *EmailBoxHandler) SendMessageToUser(nMsg string) {
	msg := tgbotapi.NewMessage(handler.user.ChatID, nMsg)
	_, err := bot.Send(msg)
	if err != nil {
		log.Println("Error sending message to user. ", err)
	}
}
