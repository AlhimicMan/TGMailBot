package main

import (
	"flag"
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"
)

/*
addaccount - Add mail account
listaccounts - List existing mail accounts
changeaccount - Change account settings (login/password/refresh frequency)
changepatterns - Change patterns for email which to notify
*/

var TGApiToken = flag.String("token", "", "Telegram API token")

const (
	AddAccount    = "Add account"
	ListAccounts  = "List accounts"
	ChangeAccount = "Change account"
	ChangePattern = "Change patterns"
)

var bot *tgbotapi.BotAPI

type UserManager struct {
	BotUsers map[int]*StoredUser
}

type StoredEmailAccount struct {
	id       int
	imapHost string
	login    string
	password string
	updateT  int
	isActive bool
}

//NotifyPatterns for filtering emails on which to send notifications
type NotifyPatterns struct {
	ID               int
	FromEmail        string
	FromPersonalName string
	Subject          string
	ContentKeyword   string
}

type StoredUser struct {
	ID     int
	Login  string
	ChatID int64
	//EmailAccounts []*StoredEmailAccount
	SearchPatterns   []string
	dialogHandler    *UserDialogHandler
	emailBoxHandlers []*EmailBoxHandler
	Patterns         []*NotifyPatterns
}

type UserDialogHandler struct {
	lastCommand     string
	lastSubCommand  string
	chatID          int
	newEmailAccount *StoredEmailAccount //Used if we adding new email account
	commandFinished bool
}

func (h *UserDialogHandler) makeTGMessage(msgText string, user *StoredUser) (*tgbotapi.MessageConfig, error) {
	msg := tgbotapi.NewMessage(user.ChatID, msgText)
	return &msg, nil
}

// AddEmailAccountHandler handles addaccount command
func (h *UserDialogHandler) AddEmailAccountHandler(inMsg *tgbotapi.Message, user *StoredUser) (*tgbotapi.MessageConfig, error) {
	msg := inMsg.Text
	if msg == "/addaccount" || h.newEmailAccount == nil || msg == AddAccount {
		h.newEmailAccount = &StoredEmailAccount{}
		h.lastCommand = "/addaccount"
		msgText := "Enter imap host in format: <host/IP>:<port>"
		return h.makeTGMessage(msgText, user)
	}
	if h.newEmailAccount.imapHost == "" {
		imapHost := msg
		hostSpl := strings.Split(msg, ":")
		imapPort := 993
		var err error
		if len(hostSpl) > 1 {
			imapPort, err = strconv.Atoi(hostSpl[1])
			if err != nil {
				msgText := fmt.Sprintf("Invalid imap port in host %s", hostSpl)
				return h.makeTGMessage(msgText, user)
			}
			imapHost = hostSpl[0]
		}

		ValidHostnameRegex := `(([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9\-]*[a-zA-Z0-9])\.)*([A-Za-z0-9]|[A-Za-z0-9][A-Za-z0-9\-]*[A-Za-z0-9])`
		ValidIPAddressRegex := `(([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])\.){3}([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])`
		hostRegexp, _ := regexp.Compile(ValidHostnameRegex)
		ipRegexp, _ := regexp.Compile(ValidIPAddressRegex)
		isHost := hostRegexp.Match([]byte(imapHost))
		isIP := ipRegexp.Match([]byte(imapHost))
		otherChecks := true
		if imapHost == "localhost" || imapHost == "127.0.0.1" {
			otherChecks = false
		}
		if !strings.Contains(imapHost, ".") {
			otherChecks = false
		}
		if !(isIP || isHost) || !otherChecks {
			msgText := fmt.Sprintf("Invalid hostname for imap server %s", msg)
			return h.makeTGMessage(msgText, user)
		}
		h.newEmailAccount.imapHost = imapHost + ":" + strconv.Itoa(imapPort)
		msgText := fmt.Sprintf("Successfully added imap host: %s\nNow sent email address:", h.newEmailAccount.imapHost)
		return h.makeTGMessage(msgText, user)
	}

	if h.newEmailAccount.login == "" {
		for _, boxHandler := range user.emailBoxHandlers {
			account := boxHandler.eAccount
			if (account.imapHost == h.newEmailAccount.imapHost) && (account.login == msg) {
				msgText := fmt.Sprintf("You already have account with this email for this host. Use /changeaccount to change account settings")
				return h.makeTGMessage(msgText, user)
			}
		}
		h.newEmailAccount.login = msg
		msgText := fmt.Sprintf("Successfully added login: %s\n Now set email account password (message with password will be removed):", h.newEmailAccount.login)
		return h.makeTGMessage(msgText, user)
	}

	if h.newEmailAccount.password == "" {
		delMsg := tgbotapi.NewDeleteMessage(inMsg.Chat.ID, inMsg.MessageID)
		_, err := bot.DeleteMessage(delMsg)
		if err != nil {
			log.Println("Error deleting password message", err)
		}
		h.newEmailAccount.password = msg
		msgText := fmt.Sprintf("Successfully added password.\nNow set update timeout in minutes:")
		return h.makeTGMessage(msgText, user)
	}

	if h.newEmailAccount.updateT == 0 {
		updTimeout, err := strconv.Atoi(msg)
		if err != nil {
			msgText := fmt.Sprintf("Invalid value for update frequency %s", msg)
			return h.makeTGMessage(msgText, user)
		}
		h.newEmailAccount.updateT = updTimeout
		h.newEmailAccount.id = int(time.Now().Unix())
		h.newEmailAccount.isActive = true
		boxHandler := NewEmailBoxHandler(h.newEmailAccount, user)
		go boxHandler.StartFetchingEmails()
		user.emailBoxHandlers = append(user.emailBoxHandlers, boxHandler)
		msgText := fmt.Sprintf("Successfully added update timeout.\nDon't forget to use /changepatterns comamnd to setup email patterns")
		h.commandFinished = true
		return h.makeTGMessage(msgText, user)
	}
	msgText := "Error creating account. Please choose command again"
	return h.makeTGMessage(msgText, user)
}

// ListAccountsHandler handles listaccounts command
func (h *UserDialogHandler) ListAccountsHandler(user *StoredUser) (*tgbotapi.MessageConfig, error) {
	h.lastCommand = "/listaccounts"
	resultStr := fmt.Sprintf("You have %d email accounts:\n", len(user.emailBoxHandlers))
	for _, boxHandler := range user.emailBoxHandlers {
		account := boxHandler.eAccount
		activeState := "false"
		if account.isActive {
			activeState = "true"
		}
		accountStr := fmt.Sprintf("Login: %s, timeout: %d min, active: %s\n", account.login, account.updateT, activeState)
		resultStr += accountStr
	}
	return h.makeTGMessage(resultStr, user)
}

// ChangeEmailAccountHandler handles changeaccount command
func (h *UserDialogHandler) ChangeEmailAccountHandler(msg string, user *StoredUser) (*tgbotapi.MessageConfig, error) {
	if msg == "/changeaccount" || msg == ChangeAccount {
		h.lastCommand = "/changeaccount"
		if len(user.emailBoxHandlers) > 0 {
			resultStr := "Select which account to change"

			accountButtons := make([]tgbotapi.InlineKeyboardButton, 0, len(user.emailBoxHandlers))
			for _, boxHandler := range user.emailBoxHandlers {
				account := boxHandler.eAccount
				activeState := "false"
				if account.isActive {
					activeState = "true"
				}
				accountStr := fmt.Sprintf("Login: %s, timeout: %d min, active: %s\n", account.login, account.updateT, activeState)
				btnData := "id_" + strconv.Itoa(account.id)
				accountButtons = append(accountButtons, tgbotapi.NewInlineKeyboardButtonData(accountStr, btnData))
			}

			aKeyboard := tgbotapi.NewInlineKeyboardMarkup(
				tgbotapi.NewInlineKeyboardRow(accountButtons...),
			)

			rMsg := tgbotapi.NewMessage(user.ChatID, resultStr)
			rMsg.ReplyMarkup = aKeyboard

			return &rMsg, nil
		} else {
			rMsg := tgbotapi.NewMessage(user.ChatID, "You don't have any email accounts")
			return &rMsg, nil
		}
	}

	if h.lastCommand == "/changeaccount" && h.lastSubCommand != "" {
		rMsgText := ""
		switch h.lastSubCommand {
		case "chpwd":
			h.newEmailAccount.password = msg
			rMsgText = "Password changed."
			if h.newEmailAccount.isActive {
				h.newEmailAccount.isActive = false
				for _, boxHandler := range user.emailBoxHandlers {
					if boxHandler.eAccount == h.newEmailAccount {
						boxHandler.Restart()
						rMsgText += " Trying to reconnect."
					}
				}
			} else {
				rMsgText += " Don't forget to activate account."
			}
			h.lastSubCommand = ""
		case "chtmt":
			nTimeout, err := strconv.Atoi(msg)
			if err != nil {
				rMsgText = "Wrong value for timeout. Please enter timeout in minutes, for example: 10"
			} else {
				h.newEmailAccount.updateT = nTimeout
				rMsgText = "Timeout changed."
				if h.newEmailAccount.isActive {
					h.newEmailAccount.isActive = false
					for _, boxHandler := range user.emailBoxHandlers {
						if boxHandler.eAccount == h.newEmailAccount {
							boxHandler.Restart()
							rMsgText += " Trying to reconnect."
						}
					}
				} else {
					rMsgText += " Don't forget to activate account."
				}
			}
		default:
			rMsgText = "Something went wrong. Please try again."
		}
		rMsg := tgbotapi.NewMessage(user.ChatID, rMsgText)

		return &rMsg, nil
	}

	return nil, nil
}

func (h *UserDialogHandler) SelectEmailAccountCallback(accountID string, user *StoredUser) (*tgbotapi.MessageConfig, error) {
	sendError := func(chatId int64) *tgbotapi.MessageConfig {
		resultStr := "We can't find selected account. Please choose from available"
		rMsg := tgbotapi.NewMessage(user.ChatID, resultStr)
		return &rMsg
	}
	accRunes := []rune(accountID)
	accIdR := accRunes[3:]
	id, err := strconv.Atoi(string(accIdR))
	if err != nil {
		return sendError(user.ChatID), nil
	}
	for _, boxHandler := range user.emailBoxHandlers {
		if boxHandler.eAccount.id == id {
			h.newEmailAccount = boxHandler.eAccount
			break
		}
	}

	if h.newEmailAccount != nil {
		resultStr := "Account selected\n"
		resultStr += fmt.Sprintf("Login: %s\n", h.newEmailAccount.login)
		resultStr += fmt.Sprintf("IMAP host: %s\n", h.newEmailAccount.imapHost)
		changeTimeoutTest := fmt.Sprintf("Change timeout (now %d min)", h.newEmailAccount.updateT)
		enableAccText := "Enable account"
		if h.newEmailAccount.isActive {
			enableAccText = "Disable account"
		}
		pKeyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Change password", "chpwd"),
				tgbotapi.NewInlineKeyboardButtonData(changeTimeoutTest, "chtmt"),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(enableAccText, "enabletrigger"),
				tgbotapi.NewInlineKeyboardButtonData("Remove account", "rmacc"),
			),
		)
		rMsg := tgbotapi.NewMessage(user.ChatID, resultStr)
		rMsg.ReplyMarkup = pKeyboard

		return &rMsg, nil
	} else {
		return sendError(user.ChatID), nil
	}
}

func (h *UserDialogHandler) ChangeAccountCommandsH(inCommand string, user *StoredUser) (*tgbotapi.MessageConfig, error) {
	var rMsgText string
	if h.newEmailAccount == nil {
		return nil, fmt.Errorf("account not selected")
	}
	switch inCommand {
	case "chpwd":
		rMsgText = "Enter new password:"
		h.lastSubCommand = "chpwd"
	case "chtmt":
		rMsgText = "Enter new timeout in minutes:"
		h.lastSubCommand = "chtmt"
	case "enabletrigger":
		if h.newEmailAccount.isActive {
			for _, boxHandler := range user.emailBoxHandlers {
				if h.newEmailAccount == boxHandler.eAccount {
					boxHandler.Stop()
					break
				}
			}
			rMsgText = "Account disabled"
		} else {
			for _, emailBox := range user.emailBoxHandlers {
				if emailBox.eAccount.id == h.newEmailAccount.id {
					emailBox.eAccount.isActive = true
					go emailBox.StartFetchingEmails()
					break
				}
			}
			rMsgText = "Account enabled"
		}
	case "rmacc":
		var accId int
		for id, emailBox := range user.emailBoxHandlers {
			if emailBox.eAccount.id == h.newEmailAccount.id {
				if emailBox.eAccount.isActive {
					emailBox.stop <- struct{}{}
				}
				accId = id
				break
			}
		}
		copy(user.emailBoxHandlers[accId:], user.emailBoxHandlers[accId+1:])         // Shift a[i+1:] left one index.
		user.emailBoxHandlers[len(user.emailBoxHandlers)-1] = nil                    // Erase last element (write zero value).
		user.emailBoxHandlers = user.emailBoxHandlers[:len(user.emailBoxHandlers)-1] // Truncate slice.
		rMsgText = "Account removed"
	default:
		rMsgText = "Please choose which parameter to change or select command from keyboard."
		h.lastSubCommand = ""
	}
	if rMsgText != "" {
		rMsg := tgbotapi.NewMessage(user.ChatID, rMsgText)
		return &rMsg, nil
	}
	return nil, nil
}

// ChangePatternsHandler handles changepatterns command
func (h *UserDialogHandler) ChangePatternsHandler(msg string, user *StoredUser) (*tgbotapi.MessageConfig, error) {
	h.lastSubCommand = ""
	if msg == "/changepatterns" || msg == ChangePattern {
		h.lastCommand = "/changepatterns"
		resultStr := "Select which pattern to change"

		patternButtons := make([][]tgbotapi.InlineKeyboardButton, 0, len(user.emailBoxHandlers))
		for _, userPattern := range user.Patterns {
			var patternStr string
			if userPattern.Subject != "" {
				patternStr = "Subject: " + userPattern.Subject
			}
			if userPattern.FromEmail != "" {
				patternStr = "From email: " + userPattern.FromEmail
			}
			if userPattern.FromPersonalName != "" {
				patternStr = "From person name: " + userPattern.FromPersonalName
			}
			if userPattern.ContentKeyword != "" {
				patternStr = "With pattern in content: " + userPattern.ContentKeyword
			}
			idStr := "pid_" + strconv.Itoa(userPattern.ID)
			patternButtons = append(patternButtons, tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData(patternStr, idStr)))
		}
		patternButtons = append(patternButtons, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Add new pattern", "newpattern")))

		pKeyboard := tgbotapi.NewInlineKeyboardMarkup(
			patternButtons...,
		)

		rMsg := tgbotapi.NewMessage(user.ChatID, resultStr)
		rMsg.ReplyMarkup = pKeyboard

		return &rMsg, nil
	}
	return nil, nil
}

func (h *UserDialogHandler) ShowPatternHandler(msg string, user *StoredUser) (*tgbotapi.MessageConfig, error) {
	h.lastSubCommand = ""
	idrunes := []rune(msg)
	patternIDStr := string(idrunes[4:])
	patternID, err := strconv.Atoi(patternIDStr)
	if err != nil {
		nMsg := tgbotapi.NewMessage(user.ChatID, "Error selecting pattern")
		return &nMsg, nil
	}
	respStr := ""
	delId := "did_" + patternIDStr
	for _, uPattern := range user.Patterns {
		if uPattern.ID == patternID {
			if uPattern.Subject != "" {
				respStr += "subject: " + uPattern.Subject
			}
			if uPattern.FromEmail != "" {
				respStr += "email: " + uPattern.FromEmail
			}
			if uPattern.FromPersonalName != "" {
				respStr += "person name: " + uPattern.FromPersonalName
			}
			if uPattern.ContentKeyword != "" {
				respStr += "keyword in content: " + uPattern.ContentKeyword
			}
			break
		}
	}
	if respStr != "" {
		respStr = "Pattern for " + respStr
		pKeyboard := tgbotapi.NewInlineKeyboardMarkup(
			tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData("Delete", delId)),
		)
		rMsg := tgbotapi.NewMessage(user.ChatID, respStr)
		rMsg.ReplyMarkup = pKeyboard

		return &rMsg, nil
	}
	return nil, nil
}

func (h *UserDialogHandler) DeletePatternHandler(msg string, user *StoredUser) (*tgbotapi.MessageConfig, error) {
	h.lastSubCommand = ""
	idrunes := []rune(msg)
	patternIDStr := string(idrunes[4:])
	patternID, err := strconv.Atoi(patternIDStr)
	if err != nil {
		nMsg := tgbotapi.NewMessage(user.ChatID, "Error selecting pattern")
		return &nMsg, nil
	}
	indexToDelete := -1
	for i, uPattern := range user.Patterns {
		if uPattern.ID == patternID {
			indexToDelete = i
		}
	}
	if indexToDelete == -1 {
		rMsg := tgbotapi.NewMessage(user.ChatID, "Cannot find pattern")
		return &rMsg, nil
	}
	copy(user.Patterns[indexToDelete:], user.Patterns[indexToDelete+1:]) // Shift a[i+1:] left one index.
	user.Patterns[len(user.Patterns)-1] = nil                            // Erase last element (write zero value).
	user.Patterns = user.Patterns[:len(user.Patterns)-1]                 // Truncate slice.
	rMsg := tgbotapi.NewMessage(user.ChatID, "Pattern removed")
	h.commandFinished = true
	return &rMsg, nil
}

func (h *UserDialogHandler) NewPatternHandler(msg string, user *StoredUser) (*tgbotapi.MessageConfig, error) {
	commands := map[string]string{
		"nsbj":        "Subject",
		"semail":      "Source email",
		"spersonname": "Source person name",
	}
	if msg == "newpattern" {
		rMsgText := "Choose for which field in email add pattern"
		inlineRows := make([][]tgbotapi.InlineKeyboardButton, 0, len(commands))
		for cmd, text := range commands {
			newRow := tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonData(text, cmd))
			inlineRows = append(inlineRows, newRow)
		}
		pKeyboard := tgbotapi.NewInlineKeyboardMarkup(inlineRows...)
		rMsg := tgbotapi.NewMessage(user.ChatID, rMsgText)
		rMsg.ReplyMarkup = pKeyboard
		h.lastSubCommand = "npch"
		return &rMsg, nil
	}
	typeText, ok := commands[msg]
	if ok {
		h.lastSubCommand = msg
		rMsgText := fmt.Sprintf("Please write pattern text for %s. We find keyword as substring in email fields.",
			strings.ToLower(typeText))
		rMsg := tgbotapi.NewMessage(user.ChatID, rMsgText)
		return &rMsg, nil
	} else {
		newPattern := &NotifyPatterns{ID: int(time.Now().Unix())}
		newVal := strings.ToLower(msg)
		switch h.lastSubCommand {
		case "nsbj":
			newPattern.Subject = newVal
		case "semail":
			if !strings.Contains(newVal, "@") {
				rMsg := tgbotapi.NewMessage(user.ChatID, "Email address is not valid")
				return &rMsg, nil
			}
			newPattern.FromEmail = newVal
		case "spersonname":
			newPattern.FromPersonalName = newVal
		}
		user.Patterns = append(user.Patterns, newPattern)
		rMsgText := "New pattern saved"
		h.lastSubCommand = ""
		rMsg := tgbotapi.NewMessage(user.ChatID, rMsgText)
		h.commandFinished = true
		return &rMsg, nil
	}
}

func (h *UserDialogHandler) SetInitialKeyboard(chatID int64) *tgbotapi.MessageConfig {
	h.commandFinished = false
	pKeyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(AddAccount),
			tgbotapi.NewKeyboardButton(ListAccounts),
		), tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(ChangeAccount),
			tgbotapi.NewKeyboardButton(ChangePattern),
		),
	)
	msgText := "Please choose command:\n"
	msgText += "/addaccount - Add new mail account\n"
	msgText += "/listaccounts - List existing mail accounts\n"
	msgText += "/changeaccount - Change account settings (password/refresh timeout) or remove account\n"
	msgText += "/changepatterns - Change patterns for email which to notify\n"
	rMsg := tgbotapi.NewMessage(chatID, msgText)
	rMsg.ReplyMarkup = pKeyboard
	return &rMsg
}

func (h *UserDialogHandler) CleanTempStores() {
	h.newEmailAccount = nil
	h.lastSubCommand = ""
}

func (mgr *UserManager) CheckUser(user *tgbotapi.User, chatID int64) *StoredUser {
	userProfile, ok := mgr.BotUsers[user.ID]
	if !ok {
		newUser := &StoredUser{
			ID:               user.ID,
			Login:            user.UserName,
			ChatID:           chatID,
			SearchPatterns:   make([]string, 0),
			dialogHandler:    &UserDialogHandler{},
			emailBoxHandlers: make([]*EmailBoxHandler, 0),
			Patterns:         make([]*NotifyPatterns, 0),
		}
		mgr.BotUsers[user.ID] = newUser
		return newUser
	}
	userProfile.ChatID = chatID
	return userProfile
}

func main() {

	flag.Parse()
	var err error
	if *TGApiToken == "" {
		log.Println("Api token is required")
		return
	}

	bot, err = tgbotapi.NewBotAPI(*TGApiToken)
	if err != nil {
		log.Panic(err)
	}

	botUsersManager := &UserManager{BotUsers: map[int]*StoredUser{}}

	log.Printf("Authorized on account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 10

	updates, err := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message == nil && update.CallbackQuery == nil { // ignore any non-Message Updates
			continue
		}
		var msg *tgbotapi.MessageConfig

		if update.CallbackQuery != nil {
			inCallback := update.CallbackQuery
			_, err := bot.AnswerCallbackQuery(tgbotapi.NewCallback(update.CallbackQuery.ID, update.CallbackQuery.Data))
			if err != nil {
				log.Println("Error making callback query ", err)
			}
			userProfile := botUsersManager.CheckUser(inCallback.From, inCallback.Message.Chat.ID)

			switch userProfile.dialogHandler.lastCommand {
			case "/changeaccount":
				var rMsg *tgbotapi.MessageConfig
				var err error
				if strings.HasPrefix(inCallback.Data, "id_") {
					rMsg, err = userProfile.dialogHandler.SelectEmailAccountCallback(inCallback.Data, userProfile)
				} else {
					rMsg, err = userProfile.dialogHandler.ChangeAccountCommandsH(inCallback.Data, userProfile)
					if err != nil {
						rMsg, err = userProfile.dialogHandler.ChangeEmailAccountHandler(ChangeAccount, userProfile)
					}
				}

				if err != nil {
					log.Println("Error listing accounts")
					continue
				} else {
					if rMsg != nil {
						msg = rMsg
					}
				}

			case "/changepatterns":
				var rMsg *tgbotapi.MessageConfig
				var err error
				if strings.HasPrefix(inCallback.Data, "pid_") {
					rMsg, err = userProfile.dialogHandler.ShowPatternHandler(inCallback.Data, userProfile)
				}
				if strings.HasPrefix(inCallback.Data, "did_") {
					rMsg, err = userProfile.dialogHandler.DeletePatternHandler(inCallback.Data, userProfile)
				}
				if inCallback.Data == "newpattern" {
					rMsg, err = userProfile.dialogHandler.NewPatternHandler(inCallback.Data, userProfile)
				}
				if userProfile.dialogHandler.lastSubCommand != "" {
					rMsg, err = userProfile.dialogHandler.NewPatternHandler(inCallback.Data, userProfile)
				}
				if err != nil {
					continue
				} else {
					if rMsg != nil {
						msg = rMsg
					}
				}
			default:
				msg = userProfile.dialogHandler.SetInitialKeyboard(userProfile.ChatID)
			}
			if msg != nil {
				_, err := bot.Send(*msg)
				if err != nil {
					log.Println("Error sending message to user")
				}
			}

		}
		if update.Message != nil {
			inMsg := update.Message

			userProfile := botUsersManager.CheckUser(update.Message.From, inMsg.Chat.ID)

			inMsgText := update.Message.Text

			currentCommand := ""
			if strings.HasPrefix(inMsgText, "/") {
				currentCommand = inMsgText
				userProfile.dialogHandler.CleanTempStores()
			} else {
				switch inMsgText {
				case AddAccount:
					currentCommand = "/addaccount"
				case ListAccounts:
					currentCommand = "/listaccounts"
				case ChangeAccount:
					currentCommand = "/changeaccount"
				case ChangePattern:
					currentCommand = "/changepatterns"
				default:
					currentCommand = userProfile.dialogHandler.lastCommand
				}
			}

			switch currentCommand {
			case "/start":
				msg = userProfile.dialogHandler.SetInitialKeyboard(userProfile.ChatID)
			case "/addaccount":
				msg, err = userProfile.dialogHandler.AddEmailAccountHandler(inMsg, userProfile)

				if err != nil {
					log.Println("Error creating account")
					continue
				}

			case "/listaccounts":
				msg, err = userProfile.dialogHandler.ListAccountsHandler(userProfile)
				if err != nil {
					log.Println("Error listing account")
					continue
				}
			case "/changeaccount":
				msg, err = userProfile.dialogHandler.ChangeEmailAccountHandler(inMsgText, userProfile)
				if err != nil {
					log.Println("Error changing account")
					continue
				}
			case "/changepatterns":
				if userProfile.dialogHandler.lastSubCommand == "" {
					msg, err = userProfile.dialogHandler.ChangePatternsHandler(inMsgText, userProfile)
				} else {
					msg, err = userProfile.dialogHandler.NewPatternHandler(inMsgText, userProfile)
				}
				if err != nil {
					log.Println("Error changing patterns")
					continue
				}
			default:
				msg = userProfile.dialogHandler.SetInitialKeyboard(userProfile.ChatID)
			}
			if msg == nil {
				msg = userProfile.dialogHandler.SetInitialKeyboard(userProfile.ChatID)
			}

			_, err := bot.Send(*msg)
			if err != nil {
				log.Println("Error sending message to user. ", err)
			}
			if userProfile.dialogHandler.commandFinished {
				userProfile.dialogHandler.commandFinished = false
				msg = userProfile.dialogHandler.SetInitialKeyboard(userProfile.ChatID)
				_, err := bot.Send(msg)
				if err != nil {
					log.Println("Error sending message to user. ", err)
				}
			}
		}

		//addaccount - Add mail account
		//listaccounts - List existing mail accounts
		//changeaccount - Change account settings (login/password/refresh frequency)
		//changepatterns - Change patterns for email which to notify

	}
}
