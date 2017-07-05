package db

import (
	"strings"
	"fmt"
	"time"
	"errors"
	"math/rand"

	_ "github.com/lib/pq"
	"database/sql"
	"github.com/jmoiron/sqlx"
)

type Config struct {
	Driver string `json:"driver"`
	Source string `json:"source"`
}

type User struct {
	ID        int          `json:"id"`
	UserName  string       `db:"username" json:"username,omitempty"`
	FirstName string       `db:"first_name" json:"first_name,omitempty"`
	LastName  string       `db:"last_name" json:"last_name,omitempty"`
	Enlisted  bool         `json:"enlisted"`
	Banned    bool         `json:"banned"`
	Admin     bool         `json:"admin"`

	exists    bool
}

type Chat struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Type  string `json:"type"`
}

type Event struct {
	ID          int          `json:"id"`
	Duration    Duration     `json:"duration"`
	ScheduledAt NullTime     `db:"scheduled_at" json:"scheduled_at"`
	StartedAt   NullTime     `db:"started_at" json:"started_at"`
	EndedAt     NullTime     `db:"ended_at" json:"ended_at"`
	Coins       int          `json:"coins"`
	Surprise    bool         `json:"surpruse"`
}

var config *Config
var db *sqlx.DB

func ScheduleEvent(coins int, start time.Time, duration Duration, surprise bool) error {
	db := GetDB()
	_, err := db.Exec(db.Rebind(`
		insert into event (
			coins, duration, scheduled_at, surprise
		) values (?, ?, ?, ?)`),
		coins, duration, start, surprise,
	)
	return err
}

func StartNewEvent(coins int, duration Duration) error {
	db := GetDB()

	tx, err := db.Beginx()
	if err != nil {
		return fmt.Errorf("failed to start transaction: %v", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(tx.Rebind(`
		insert into event (
			coins, duration, started_at, surprise
		) values (?, ?, ?, ?)`),
		coins, duration, time.Now(), true,
	)
	if err != nil {
		return fmt.Errorf("failed to insert event: %v", err)
	}

	var event Event
	if err = tx.Get(&event, "select * from event where ended_at is null"); err != nil {
		return fmt.Errorf("event inserted, but could not be found immediatly after: %v", err)
	}

	if err := event.addParticipants(tx); err != nil {
		return fmt.Errorf("failed to add participants: %v", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit the event: %v", err)
	}

	return nil
}

func (e *Event) addParticipants(tx *sqlx.Tx) error {
	var ids []int
	err := tx.Select(&ids, "select id from botuser where not banned and enlisted")
	if err != nil {
		return fmt.Errorf("failed to select eligible users for coin distribution: %v", err)
	}

	if len(ids) == 0 {
		return nil
	}

	coinsPerUser := e.Coins / len(ids)
	volatility := 0
	if e.Coins % len(ids) != 0 {
		volatility = 1
	}

	for _, uid := range ids {
		coins := coinsPerUser + rand.Intn(volatility + 1)
		_, err := tx.Exec(tx.Rebind(`
			insert into participant (
				event_id, user_id, coins
			) values (?, ?, ?)`),
			e.ID, uid, coins,
		)
		if err != nil {
			return fmt.Errorf("failed to add user to event participants: %v", err)
		}
	}
	return nil
}

func (e *Event) Start() error {
	if e.StartedAt.Valid {
		return errors.New("already started")
	}
	t := NewNullTime(time.Now())
	db := GetDB()

	tx, err := db.Beginx()
	if err != nil {
		return fmt.Errorf("failed to start transaction: %v", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(
		tx.Rebind("update event set started_at = ? where id = ?"),
		t, e.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update event status: %v", err)
	}

	if err := e.addParticipants(tx); err != nil {
		return fmt.Errorf("failed to add participants: %v", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit the event: %v", err)
	}

	e.StartedAt = t
	return nil
}

func (e *Event) End() error {
	if e.EndedAt.Valid {
		return errors.New("already ended")
	}
	t := NewNullTime(time.Now())
	db := GetDB()
	_, err := db.Exec(
		db.Rebind("update event set ended_at = ? where id = ?"),
		t, e.ID,
	)
	if err == nil {
		e.EndedAt = t
	}
	return err
}

func GetCurrentEvent() *Event {
	var event Event

	db := GetDB()
	err := db.Get(&event, "select * from event where ended_at is null")

	if err == sql.ErrNoRows {
		return nil
	}

	if err != nil {
		panic(err)
		return nil
	}

	return &event
}

func GetDB() *sqlx.DB {
	if db == nil {
		if config == nil {
			panic("please call db.Init() before any other method")
		}

		var err error
		db, err = sqlx.Open(config.Driver, config.Source)
		if err != nil {
			panic(err)
		}
	}

	return db
}

func GetUser(id int) *User {
	var user User
	db := GetDB()
	err := db.Get(&user, db.Rebind("select * from botuser where id = ?"), id)

	if err == sql.ErrNoRows {
		return nil
	}

	if err != nil {
		return nil
	}

	user.exists = true
	return &user
}

func GetUserByName(name string) *User {
	var user User
	db := GetDB()
	err := db.Get(&user, db.Rebind("select * from botuser where username = ?"), name)

	if err == sql.ErrNoRows {
		return nil
	}

	if err != nil {
		return nil
	}

	user.exists = true
	return &user
}

func GetUsers(banned bool) ([]User, error) {
	var users []User
	db := GetDB()

	err := db.Select(&users, db.Rebind("select * from botuser where banned = ? order by username"), banned)
	if err != nil {
		return nil, err
	}

	return users, nil
}

func GetUserCount(banned bool) (int, error) {
	var count int
	db := GetDB()

	err := db.Get(&count, db.Rebind("select count(*) from botuser where banned = ?"), banned)
	if err != nil {
		return 0, err
	}

	return count, nil
}

func (u *User) Put() error {
	db := GetDB()
	if u.exists {
		_, err := db.Exec(db.Rebind(`
			update botuser
				set username = ?,
				first_name = ?,
				last_name = ?,
				banned = ?,
				admin = ?
			where id = ?`),
			u.UserName,
			u.FirstName,
			u.LastName,
			u.Banned,
			u.Admin,
			u.ID,
		)
		return err
	} else {
		_, err := db.Exec(db.Rebind(`
			insert into botuser (
				id, username, first_name, last_name,
				banned, admin
			) values (?, ?, ?, ?, ?, ?)`),
			u.ID,
			u.UserName,
			u.FirstName,
			u.LastName,
			u.Banned,
			u.Admin,
		)
		if err == nil {
			u.exists = true
		}
		return err
	}
}

func (u *User) NameAndTags() string {
	var tags []string
	if u.Banned {
		tags = append(tags, "banned")
	}
	if u.Admin {
		tags = append(tags, "admin")
	}

	if len(tags) > 0 {
		return fmt.Sprintf("%s (%s)", u.UserName, strings.Join(tags, ", "))
	} else {
		return u.UserName
	}
}

func Init(newConfig *Config) {
	config = newConfig
}
