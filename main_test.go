package main

import (
	"github.com/emersion/go-imap"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"strings"
	"testing"
)

func TestEmailBoxHandler_CheckPatterns(t *testing.T) {
	type tCase struct {
		Message imap.Message
		Result  bool
	}

	testPatterns := []*NotifyPatterns{
		{
			ID:               0,
			FromEmail:        "test@mail.test",
			FromPersonalName: "",
			Subject:          "",
		},
		{
			ID:               1,
			FromEmail:        "",
			FromPersonalName: "test sender",
			Subject:          "",
		},
		{
			ID:               2,
			FromEmail:        "",
			FromPersonalName: "tester",
			Subject:          "",
		},
		{
			ID:               3,
			FromEmail:        "",
			FromPersonalName: "",
			Subject:          "important",
		},
	}

	testCases := []tCase{
		{
			Message: imap.Message{
				SeqNum: 0,
				Envelope: &imap.Envelope{
					Subject: "",
					From: []*imap.Address{
						{
							PersonalName: "Test2 person",
							AtDomainList: "",
							MailboxName:  "test2@mail.test",
							HostName:     "",
						},
						{
							PersonalName: "Test person",
							AtDomainList: "",
							MailboxName:  "test@mail.test",
							HostName:     "",
						},
					},
					Sender: nil,
				}},
			Result: true,
		},
		{
			Message: imap.Message{
				SeqNum: 1,
				Envelope: &imap.Envelope{
					Subject: "",
					From: []*imap.Address{{
						PersonalName: "Test sender",
						AtDomainList: "",
						MailboxName:  "supertest@mail.test",
						HostName:     "",
					}},
					Sender: nil,
				}},
			Result: true,
		},
		{
			Message: imap.Message{
				SeqNum: 2,
				Envelope: &imap.Envelope{
					Subject: "",
					From: []*imap.Address{{
						PersonalName: "Test manager",
						AtDomainList: "",
						MailboxName:  "mtest@mail.test",
						HostName:     "",
					}},
					Sender: nil,
				}},
			Result: false,
		},
		{
			Message: imap.Message{
				SeqNum: 3,
				Envelope: &imap.Envelope{
					Subject: "",
					From: []*imap.Address{{
						PersonalName: "Tester intern",
						AtDomainList: "",
						MailboxName:  "intern@mail.test",
						HostName:     "",
					}},
					Sender: nil,
				}},
			Result: false,
		},
		{
			Message: imap.Message{
				SeqNum: 4,
				Envelope: &imap.Envelope{
					Subject: "Important",
					From: []*imap.Address{{
						PersonalName: "Tester intern",
						AtDomainList: "",
						MailboxName:  "intern@mail.test",
						HostName:     "",
					}},
					Sender: nil,
				}},
			Result: true,
		},
		{
			Message: imap.Message{
				SeqNum: 5,
				Envelope: &imap.Envelope{
					Subject: "Flood",
					From: []*imap.Address{{
						PersonalName: "Tester intern",
						AtDomainList: "",
						MailboxName:  "intern@mail.test",
						HostName:     "",
					}},
					Sender: nil,
				}},
			Result: false,
		},
	}

	boxHandler := EmailBoxHandler{
		user: &StoredUser{
			ID:               0,
			Login:            "",
			ChatID:           0,
			LastMessageId:    0,
			SearchPatterns:   nil,
			dialogHandler:    nil,
			emailBoxHandlers: nil,
			Patterns:         testPatterns,
		},
	}
	for i, testCase := range testCases {
		checkResult := boxHandler.CheckPatterns(&testCase.Message)
		if checkResult != testCase.Result {
			t.Errorf("[%d] result mismatch. want: %t, have: %t", i, testCase.Result, checkResult)
		}
	}
}

func TestAddingAccount(t *testing.T) {
	bot = &tgbotapi.BotAPI{} //Bad idea, we could receive errors in deleting email

	type tCase struct {
		textMessages  []string
		resultMsgText string //Must contain, not match
	}
	newAccount := StoredEmailAccount{
		id:       0,
		imapHost: "imap.test.com",
		login:    "test@test.com",
		password: "Test123",
		updateT:  3,
		isActive: false,
	}

	testCases := []tCase{
		{
			textMessages:  []string{AddAccount, "test"},
			resultMsgText: "Wrong format for login, please set <login>@<domain>",
		},
		{
			textMessages:  []string{AddAccount, newAccount.login, "localhost"},
			resultMsgText: "Invalid hostname for imap server localhost",
		},
		{
			textMessages:  []string{AddAccount, newAccount.login, "imap.test.com:rr"},
			resultMsgText: "Invalid imap port in host",
		},
		{
			textMessages:  []string{AddAccount, newAccount.login, newAccount.imapHost, newAccount.password, "1min"},
			resultMsgText: "Invalid value for update frequency 1min",
		},
		{
			textMessages:  []string{AddAccount, newAccount.login, newAccount.imapHost, newAccount.password, "1"},
			resultMsgText: "Account created",
		},
		{
			textMessages:  []string{AddAccount, newAccount.login, newAccount.imapHost, newAccount.password, "1", AddAccount, newAccount.login, newAccount.imapHost},
			resultMsgText: "You already have account with this email for this host",
		},
	}
	dialogHandler := UserDialogHandler{
		lastCommand:     "",
		lastSubCommand:  "",
		chatID:          0,
		newEmailAccount: nil,
		commandFinished: false,
	}
	user := &StoredUser{}
	for i, tCase := range testCases {
		var lastMsgText string
		for _, msgText := range tCase.textMessages {
			inMsg := &tgbotapi.Message{
				Text: msgText,
			}
			outMsg, _ := dialogHandler.AddEmailAccountHandler(inMsg, user)
			lastMsgText = outMsg.Text
		}
		if !strings.Contains(lastMsgText, tCase.resultMsgText) {
			t.Errorf("[%d] Text mesmatch.\nWant: %s\nHave:%s", i, tCase.resultMsgText, lastMsgText)
		}
	}
}
