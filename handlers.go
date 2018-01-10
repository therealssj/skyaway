package skyaway

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"strconv"
	"time"

	"github.com/bcampbell/fuzzytime"
	"github.com/skycoin/skycoin/src/cipher"
	"gopkg.in/telegram-bot-api.v4"
)

// Handler for help command
func (bot *Bot) handleCommandHelp(ctx *Context, command, args string) error {
	// Indentation messes up how the text is shown in chat.
	if ctx.User.Admin {
		return bot.Reply(ctx, `
/start
/help - this text
/settings

---- Event Commands ----
/scheduleevent [coins] [ISO timestamp, or human readable] [duration] [surprise] - start an event at timestamp and duration in hours
/cancelevent - cancel a scheduled event
/stopevent - stop current event
/startevent [number of coins] [duration] - start an event immediately
/listevent  - list the current event (admins can also see surprise events)

---- User Commands ----
/adduser [username or id] - force add user to eligible list
/makeadmin [username] - make a user an admin
/removeadmin [username] - remove user from admin position
/banuser [username or id] - blacklist user from eligible list
/unbanuser [username or id] - remove user from blacklist
/usercount - return number of users
/users - return all users in list
/bannedusers - return all users in banned list
/listadmins - return list of all admins
/listwinners [event] [number] - choose random number of winners from the participating list
								event can have values last, current or numeric event id
/resetwinners [event] - event can have values last, current or numeric event id
/registeraddress - register/update your sky address

---- Announcement Commands ----
/announce [msg] - send announcement
/announceevent - force send current scheduled or ongoing event announcement
`)
	}

	return bot.Reply(ctx, `
/start
/help - this text
/listevent - lists the current event
/registeraddress - register/update your sky address`)
}

// Handler for start command
func (bot *Bot) handleCommandStart(ctx *Context, command, args string) error {
	helpCommand := "/help"
	if !ctx.message.Chat.IsPrivate() {
		helpCommand += "@" + bot.telegram.Self.UserName
	}
	return bot.Reply(ctx, fmt.Sprintf(
		`Hey, this is a skycoin giveaway bot!
Type %s for details.`,
		helpCommand,
	))
}

// Handler for adduser comamnd
func (bot *Bot) handleCommandAddUser(ctx *Context, command, args string) error {
	identifier := args
	dbuser := bot.db.GetUserByNameOrId(identifier)
	if dbuser == nil {
		return bot.Reply(ctx, "no user by that name or id")
	}
	return bot.enableUserVerbosely(ctx, dbuser)
}

// Handler for promoteuser comamnd
func (bot *Bot) handleCommandMakeAdmin(ctx *Context, command, args string) error {
	identifier := args
	dbuser := bot.db.GetUserByNameOrId(identifier)
	if dbuser == nil {
		return bot.Reply(ctx, "no user by that name")
	}
	dbuser.Admin = true

	bot.db.PutUser(dbuser)
	return bot.Reply(ctx, fmt.Sprintf("User %s is now an admin", identifier))
}

// Handler for promoteuser comamnd
func (bot *Bot) handleCommandRemoveAdmin(ctx *Context, command, args string) error {
	identifier := args
	dbuser := bot.db.GetUserByNameOrId(identifier)
	if dbuser == nil {
		return bot.Reply(ctx, "no user by that name")
	}
	dbuser.Admin = false
	bot.db.PutUser(dbuser)
	return bot.Reply(ctx, fmt.Sprintf("User %s is not an admin anymore", identifier))
}

// Handler for announce command
func (bot *Bot) handleCommandAnnounce(ctx *Context, command, args string) error {
	msg := strings.TrimSpace(args)
	if msg == "" {
		return fmt.Errorf("cannot announce an empty message")
	}
	if err := bot.Send(ctx, "yell", "text", msg); err != nil {
		return fmt.Errorf("failed to announce: %v", err)
	}

	return bot.Reply(ctx, "done")
}

// Handler for announceevent command
func (bot *Bot) handleCommandAnnounceEvent(ctx *Context, command, args string) error {
	event := bot.db.GetCurrentEvent()
	if event == nil {
		return bot.Reply(ctx, "nothing to announce")
	}

	md := formatEventAsMarkdown(event, true)
	if err := bot.Send(ctx, "yell", "markdown", md); err != nil {
		return fmt.Errorf("failed to announce event: %v", err)
	}

	return bot.Reply(ctx, "done")
}

// Handler for listvents command
func (bot *Bot) handleCommandListEvent(ctx *Context, command, args string) error {
	event := bot.db.GetCurrentEvent()

	if event == nil {
		return bot.Reply(ctx, "No events")
	}

	// If event is a surprise event don't  show it if the
	// user is not an admin
	if event.Surprise && !ctx.User.Admin {
		return bot.Reply(ctx, "No events")
	}

	// Check what type of event it is
	if event.StartedAt.Valid {
		fmt.Println(event.StartedAt.Time.Add(event.Duration.Duration))
		return bot.Reply(ctx, fmt.Sprintf("Current event ends at  %s", event.StartedAt.Time.Add(event.Duration.Duration)))
	} else if event.ScheduledAt.Valid {
		return bot.Reply(ctx, fmt.Sprintf("Upcoming event starts at %s", event.ScheduledAt.Time))
	}

	log.Print("The current event is not scheduled, not started and not ended. That should not have happened.")
	// If the user is an admin tell that there is an error
	if ctx.User.Admin {
		return bot.Reply(ctx, "The current event has an error.")
	}

	return bot.Reply(ctx, "No events")
}

// Handler for ban user command
func (bot *Bot) handleCommandBanUser(ctx *Context, command, args string) error {
	identifer := args
	user := bot.db.GetUserByNameOrId(identifer)
	if user == nil {
		return bot.Reply(ctx, "no user by that name or id")
	}
	if !user.Banned {
		user.Banned = true
		if err := bot.db.PutUser(user); err != nil {
			return fmt.Errorf("failed to change user status: %v", err)
		}
	}
	return bot.Reply(ctx, user.NameAndTags())
}

// Handler for unban user command
func (bot *Bot) handleCommandUnBanUser(ctx *Context, command, args string) error {
	identifer := args
	user := bot.db.GetUserByNameOrId(identifer)
	if user == nil {
		return bot.Reply(ctx, "no user by that name or id")
	}
	if user.Banned {
		user.Banned = false
		if err := bot.db.PutUser(user); err != nil {
			return fmt.Errorf("failed to change user status: %v", err)
		}
	}
	return bot.Reply(ctx, fmt.Sprintf("unbanned user %s", user.NameAndTags()))
}

// Handler for cancelevent command
func (bot *Bot) handleCommandCancelEvent(ctx *Context, command, args string) error {
	event := bot.db.GetCurrentEvent()
	if event == nil {
		return bot.Reply(ctx, "nothing to cancel")
	}

	if event.StartedAt.Valid {
		return bot.ReplyAboutEvent(
			ctx,
			"the event has already started, use /stopevent instead",
			event,
		)
	}

	if _, err := bot.EndCurrentEvent(); err != nil {
		return fmt.Errorf("failed to cancel the event: %v", err)
	}

	return bot.ReplyAboutEvent(ctx, "event cancelled", event)
}

// Handler for scheduleevent command
func (bot *Bot) handleCommandScheduleEvent(ctx *Context, command, args string) error {
	coins, start, duration, surprise, err := parseScheduleEventArgs(args)
	if err != nil {
		return fmt.Errorf("could not understand: %v", err)
	}

	haveCurrent, err := bot.complainIfHaveCurrentEvent(ctx)
	if haveCurrent || err != nil {
		return err
	}

	err = bot.db.ScheduleEvent(coins, start, duration, surprise)
	if err != nil {
		return fmt.Errorf("failed to schedule event: %v", err)
	}

	event := bot.db.GetCurrentEvent()
	if event == nil {
		return fmt.Errorf("event was not scheduled due to reasons unknown")
	}
	defer bot.Reschedule()

	if !surprise {
		bot.AnnounceEventWithTitle(event, "A new event has been scheduled!")
	}
	return bot.ReplyAboutEvent(ctx, "event scheduled", event)
}

// Handler for settings command
func (bot *Bot) handleCommandSettings(ctx *Context, command, args string) error {
	chat, err := bot.telegram.GetChat(tgbotapi.ChatConfig{bot.config.ChatID, ""})
	if err != nil {
		return fmt.Errorf("failed to get chat info: %v", err)
	}

	settings := map[string]interface{}{
		"bot": map[string]interface{}{
			"id":   bot.telegram.Self.ID,
			"name": bot.telegram.Self.UserName,
		},
		"chat": map[string]interface{}{
			"id":    chat.ID,
			"type":  chat.Type,
			"title": chat.Title,
		},
	}
	encoded, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode current settings into json: %v", err)
	}
	return bot.Reply(ctx, fmt.Sprintf("current settings: %s", string(encoded)))
}

// Handler for startevent commnad
func (bot *Bot) handleCommandStartEvent(ctx *Context, command, args string) error {
	words := strings.Fields(args)
	coins, err := strconv.Atoi(words[0])

	if err != nil {
		return bot.Reply(ctx, "malformed coins format: use an integer number")
	}

	dur, err := time.ParseDuration(words[1])

	if err != nil {
		return bot.Reply(ctx, "malformed duration format")
	}

	duration := Duration{
		dur,
		true,
	}

	event, err := bot.StartNewEvent(coins, duration)
	if err == EventExists {
		return bot.ReplyAboutEvent(ctx, "already have an event", event)
	}
	if err != nil {
		return err
	}

	return bot.ReplyAboutEvent(ctx, "event started", event)
}

// Handler for stopevent command
func (bot *Bot) handleCommandStopEvent(ctx *Context, command, args string) error {
	event := bot.db.GetCurrentEvent()
	if event == nil {
		return bot.Reply(ctx, "nothing to stop")
	}

	if !event.StartedAt.Valid {
		return bot.ReplyAboutEvent(
			ctx,
			"the event has not started yet, use /cancelevent instead",
			event,
		)
	}

	if _, err := bot.EndCurrentEvent(); err != nil {
		return fmt.Errorf("failed to stop the event: %v", err)
	}

	return bot.ReplyAboutEvent(ctx, "event stopped", event)
}

func (bot *Bot) handleCommandCurrentEvent(ctx *Context, banned bool) error {
	users, err := bot.db.GetUsers(banned)

	if err != nil {
		return fmt.Errorf("failed to get users from db: %v", err)
	}

	var lines []string
	for i, user := range users {
		lines = append(lines, fmt.Sprintf(
			"%d. %d: %s", (i+1), user.ID, user.NameAndTags(),
		))
	}
	if len(lines) > 0 {
		return bot.Reply(ctx, strings.Join(lines, "\n"))
	} else {
		return bot.Reply(ctx, "no users in the list")
	}
}

// Handler for usercount command
func (bot *Bot) handleCommandUserCount(ctx *Context, command, args string) error {
	banned := false
	count, err := bot.db.GetUserCount(banned)

	if err != nil {
		return fmt.Errorf("failed to get user count from db: %v", err)
	}

	return bot.Reply(ctx, strconv.Itoa(count))
}

// Handler for users command
func (bot *Bot) handleCommandUsersParsed(ctx *Context, banned bool) error {
	users, err := bot.db.GetUsers(banned)

	if err != nil {
		return fmt.Errorf("failed to get users from db: %v", err)
	}

	var lines []string
	for i, user := range users {
		lines = append(lines, fmt.Sprintf(
			"%d. %d: %s", (i+1), user.ID, user.NameAndTags(),
		))
	}
	if len(lines) > 0 {
		return bot.Reply(ctx, strings.Join(lines, "\n"))
	} else {
		return bot.Reply(ctx, "no users in the list")
	}
}

func (bot *Bot) handleCommandListAdmins(ctx *Context, command, args string) error {
	admins, err := bot.db.GetAdmins()

	if err != nil {
		return fmt.Errorf("failed to get admins from db: %v", err)
	}

	var lines []string

	for i, admin := range admins {
		lines = append(lines, fmt.Sprintf(
			"%d. %d: %s", (i+1), admin.ID, admin.NameAndTags(),
		))
	}
	if len(lines) > 0 {
		return bot.Reply(ctx, strings.Join(lines, "\n"))
	} else {
		return bot.Reply(ctx, "no admins")
	}
}

// Handler for listwinners command
func (bot *Bot) handleCommandListWinners(ctx *Context, command, args string) error {
	var eventID int
	var err error

	words := strings.Fields(args)

	if len(words) > 2 {
		return bot.Reply(ctx, "invalid number of arguments")
	}

	// Get number of winners to choose
	num, err := strconv.Atoi(words[1])
	if err != nil {
		return bot.Reply(ctx, fmt.Sprintf("invalid number argument: %s", words[1]))
	}

	// get last or current event id
	event := words[0]
	if event == "last" {
		event := bot.db.GetLastEvent()
		eventID = event.ID
	} else if event == "current" {
		event := bot.db.GetCurrentEvent()
		eventID = event.ID
	} else {
		// check if input argument is an integer
		eventID, err = strconv.Atoi(event)
		if err != nil {
			return bot.Reply(ctx, fmt.Sprintf("invalid event ID: %s", words[0]))
		}
	}

	// Check if we already have winners for particular event
	winnersAlready, err := bot.db.WinnersAlreadySelected(eventID)
	if err != nil {
		return bot.Reply(ctx, fmt.Sprintf("failed to get winners from db: %v", err))
	}

	var winners []Participant
	if winnersAlready {
		winners, err = bot.db.GetParticipants(eventID, true)
		if err != nil {
			return fmt.Errorf("failed to get existing winners from db: %v", err)
		}
	} else {
		var participants []Participant
		participants, err = bot.db.GetParticipants(eventID, false)
		if err != nil {
			return fmt.Errorf("failed to get winners from db: %v", err)
		}

		// Select n random winners
		winners = getRandomWinners(participants, num)

		// Create a list of user ids
		winnerList := make([]string, num)
		for _, winner := range winners {
			winnerList = append(winnerList, strconv.Itoa(winner.UserID))
		}

		// Set winners
		err = bot.db.SetWinners(eventID, strings.Join(winnerList, ","))
		if err != nil {
			return fmt.Errorf("failed to set winners in db: %v", err)
		}
	}

	var lines []string
	for i, winner := range winners {
		lines = append(lines, fmt.Sprintf(
			"%d. %d: %s: coinswon -> %d, skyaddress -> (%s)", (i+1), winner.UserID, winner.UserName, winner.Coins, winner.Address,
		))
	}
	if len(lines) > 0 {
		return bot.Reply(ctx, strings.Join(lines, "\n"))
	} else {
		return bot.Reply(ctx, "no winners, that's weird")
	}
}

// Handler for resetwinners command
func (bot *Bot) handleCommandResetWinners(ctx *Context, command, args string) error {
	var eventID int
	var err error
	words := strings.Fields(args)

	if len(words) != 1 {
		return fmt.Errorf("invalid number of arguments: %v", len(words))
	}

	// get last or current event id
	event := words[0]
	if event == "last" {
		event := bot.db.GetLastEvent()
		eventID = event.ID
	} else if event == "current" {
		event := bot.db.GetCurrentEvent()
		eventID = event.ID
	} else {
		// check if input argument is an integer
		eventID, err = strconv.Atoi(event)
		if err != nil {
			return bot.Reply(ctx, fmt.Sprintf("invalid event ID: %s", words[0]))
		}
	}

	err = bot.db.ResetWinners(eventID)
	if err != nil {
		return fmt.Errorf("unable to reset winners: %v", err)
	}

	return bot.Reply(ctx, fmt.Sprintf("Reset winners for event %v done", eventID))
}

// Handler for registeraddress command
func (bot *Bot) handleCommandRegisterAddress(ctx *Context, command, args string) error {
	skyAddr := strings.Fields(args)[0]

	// Parse address to check validity
	_, err := cipher.DecodeBase58Address(skyAddr)
	if err != nil {
		return fmt.Errorf("invalid sky address: %v", err)
	}

	// Insert sky address into database
	err = bot.db.PutSkyAddr(ctx.User.ID, skyAddr)
	if err != nil {
		return fmt.Errorf("failed to add sky address to db: %v", err)
	}

	return bot.Reply(ctx, fmt.Sprintf("Address %v registered!", skyAddr))
}

// Handler for listvars command
func (bot *Bot) handleCommandListVars(ctx *Context, command, args string) error {
	return bot.Reply(ctx, "announce_every")
}

// Handler for setvar command
func (bot *Bot) handleCommandSetVar(ctx *Context, command, args string) error {
	return nil
}

// Handler for getvar command
func (bot *Bot) handleCommandGetVar(ctx *Context, command, args string) error {

	return nil
}

func (bot *Bot) handleDirectMessageFallback(ctx *Context, text string) (bool, error) {
	event := bot.db.GetCurrentEvent()

	if event != nil {
		started := event.StartedAt.Valid
		canTellWhen := !event.Surprise

		if !started {
			// Dont show event if its a surprise event
			if canTellWhen {
				return true, bot.Reply(ctx, fmt.Sprintf(
					"event starts in %s",
					niceDuration(time.Until(event.ScheduledAt.Time)),
				))
			}
		} else {
			// If there is a current event going on then show time until end
			endsAt := event.StartedAt.Time.Add(event.Duration.Duration)
			return true, bot.Reply(ctx, fmt.Sprintf("Current event ends in %s", niceDuration(time.Until(endsAt))))
		}
	}

	return true, bot.Reply(ctx, "no upcoming events, check back later")
}

func (bot *Bot) AddPrivateMessageHandler(handler MessageHandler) {
	bot.privateMessageHandlers = append(bot.privateMessageHandlers, handler)
}

func (bot *Bot) AddGroupMessageHandler(handler MessageHandler) {
	bot.groupMessageHandlers = append(bot.groupMessageHandlers, handler)
}

func (bot *Bot) enableUserVerbosely(ctx *Context, dbuser *User) error {
	actions, err := bot.enableUser(dbuser)
	if err != nil {
		return fmt.Errorf("failed to enable user: %v", err)
	}
	if len(actions) > 0 {
		return bot.Reply(ctx, strings.Join(actions, ", "))
	}
	return bot.Reply(ctx, "no action required")
}

func (bot *Bot) complainIfHaveCurrentEvent(ctx *Context) (bool, error) {
	if event := bot.db.GetCurrentEvent(); event != nil {
		if event.StartedAt.Valid {
			return true, bot.ReplyAboutEvent(ctx, "already have an active event", event)
		} else {
			return true, bot.ReplyAboutEvent(ctx, "already have an event in schedule", event)
		}
	}
	return false, nil
}

func parseScheduleEventArgs(args string) (coins int, start time.Time, duration Duration, surprise bool, err error) {
	words := strings.Fields(args)
	if len(words) < 2 {
		err = fmt.Errorf("insufficient arguments")
		return
	}

	coins, err = strconv.Atoi(words[0])
	if err != nil {
		err = fmt.Errorf("could not parse the number of coins: %v", err)
		return
	}

	surprise = words[len(words)-1] == "surprise"
	if surprise {
		// cut out the first and last word
		words = words[1 : len(words)-1]
	} else {
		// cut out the first word
		words = words[1:len(words)]
	}

	dur, err := parseDuration(words[len(words)-1])
	if err != nil {
		err = fmt.Errorf("malformed duration format: %s", words[len(words)-1])
		return
	}

	duration = Duration{
		dur,
		true,
	}

	timestr := strings.Join(words, " ")
	ft, _, err := fuzzytime.Extract(timestr)
	if ft.Empty() {
		err = fmt.Errorf("unsupported datetime format: %v", timestr)
		return
	}

	var hour, minute, second int
	var loc *time.Location
	if ft.Time.HasHour() {
		hour = ft.Time.Hour()
	}
	if ft.Time.HasMinute() {
		minute = ft.Time.Minute()
	}
	if ft.Time.HasSecond() {
		second = ft.Time.Second()
	}
	if ft.Time.HasTZOffset() {
		loc = time.FixedZone("", ft.Time.TZOffset())
	} else {
		loc = time.UTC
	}

	if ft.HasFullDate() {
		start = time.Date(
			ft.Date.Year(),
			time.Month(ft.Date.Month()),
			ft.Date.Day(),
			hour, minute, second, 0,
			loc,
		)
	} else {
		year, month, day := time.Now().In(loc).Date()
		start = time.Date(
			year, month, day,
			hour, minute, second, 0,
			loc,
		)
		if start.Before(time.Now()) {
			start = start.AddDate(0, 0, 1)
		}
	}

	if start.Before(time.Now()) {
		err = fmt.Errorf("%s is in the past", start.String())
		return
	}

	return
}
