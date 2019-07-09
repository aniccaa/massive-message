package repository

import (
	"container/list"
	"fmt"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	log "github.com/sirupsen/logrus"
	"massive-message/notification/sdk"
	"time"
)

var (
	connection *gorm.DB
	tables     = []TableInfo{
		{Name: "Event", Info: new(Event)},
		{Name: "Alert", Info: new(Alert)},
	}
)

// TableInfo The tables in DB.
type TableInfo struct {
	Name string
	Info interface{}
}

// Event is the represents the table for event objects.
type Event struct {
	ID          string    `gorm:"column:ID;primary_key"`
	Key         string    `gorm:"column:Key;index"`
	URL         string    `gorm:"column:URL;index"`
	VersusKey   string    `gorm:"column:VersusKey"`
	Type        string    `gorm:"column:Type"`
	GeneratedAt time.Time `gorm:"column:GeneratedAt"`
	Severity    string    `gorm:"column:Severity"`
	Description string    `gorm:"column:Description"`
}

// TableName will set the table name.
func (Event) TableName() string {
	return "Event"
}

// Alert is the represents the table for alert objects.
type Alert struct {
	ID          string    `gorm:"column:ID;primary_key"`
	Key         string    `gorm:"column:Key;index"`
	URL         string    `gorm:"column:URL;index"`
	VersusKey   string    `gorm:"column:VersusKey"`
	Type        string    `gorm:"column:Type"`
	GeneratedAt time.Time `gorm:"column:GeneratedAt"`
	Severity    string    `gorm:"column:Severity"`
	Description string    `gorm:"column:Description"`
}

// TableName will set the table name.
func (Alert) TableName() string {
	return "Alert"
}

func newAlert(o *sdk.Notification) *Alert {
	ret := Alert{}
	ret.ID = uuid.New().String()
	ret.Key = o.Key
	ret.VersusKey = o.VersusKey
	ret.URL = o.URL
	ret.Type = o.Type
	ret.GeneratedAt = o.GeneratedAt
	ret.Severity = o.Severity
	ret.Description = o.Description
	return &ret
}

func newEvent(o *sdk.Notification) *Event {
	ret := Event{}
	ret.ID = uuid.New().String()
	ret.Key = o.Key
	ret.VersusKey = o.VersusKey
	ret.URL = o.URL
	ret.Type = o.Type
	ret.GeneratedAt = o.GeneratedAt
	ret.Severity = o.Severity
	ret.Description = o.Description
	return &ret
}

// Init perform the initialization work.
func Init() error {
	var err error
	if connection == nil {
		log.Info("[Notification-Repository] Init DB connection.")
		args := fmt.Sprintf("host=postgres port=5432 user=postgres dbname=notification sslmode=disable password=iforgot")
		connection, err = gorm.Open("postgres", args)
		if err != nil {
			log.WithFields(log.Fields{"error": err}).Error("[Notification-Repository] DB open failed.")
			return err
		}
		// connection.LogMode(true)
		connection.SingularTable(true)
	} else {
		log.Info("[Notification-Repository] DB connection exist.")
	}
	return nil
}

// CreateTables creates the tables.
func CreateTables() error {
	for _, v := range tables {
		if err := connection.CreateTable(v.Info).Error; err != nil {
			log.WithFields(log.Fields{"Table": v.Name, "error": err}).Error("[Notification-Repository] create table failed.")
			return err
		}
	}
	return nil
}

// DropTablesIfExist drops tables if they are exist.
func DropTablesIfExist() error {
	for _, v := range tables {
		if err := connection.DropTableIfExists(v.Info).Error; err != nil {
			log.WithFields(log.Fields{"Table": v.Name, "error": err}).Error("[Notification-Repository] remove table failed.")
			return err
		}
	}
	return nil
}

// SaveNotification saves the notification into the database.
func SaveNotification(o *sdk.Notification) error {
	var entity interface{}
	if o.Type == "Alert" {
		entity = newAlert(o)
	} else {
		entity = newEvent(o)
	}
	if err := connection.Create(entity).Error; err != nil {
		log.WithFields(log.Fields{"error": err}).Error("[Notification-Repository] Save notification failed.")
		return err
	}
	return nil
}

// GetTargetsHaveAlert returns all the targets that have alerts.
// On error, return nil.
func GetTargetsHaveAlert() ([]string, error) {
	sqlResult := []Alert{}
	if err := connection.Select("DISTINCT(\"URL\")").Find(&sqlResult).Error; err != nil {
		log.WithFields(log.Fields{"error": err}).Warn("[Notification-Repository] Get targets that having alerts failed.")
		return nil, err
	}
	ret := []string{}
	for _, v := range sqlResult {
		ret = append(ret, v.URL)
	}
	return ret, nil
}

// CombineAlertsByURL finds all the alerts that matches the url, and remove the ones that can be removed.
// The ones that can be removed can be found like this:
// 1. Sorts the alerts by GeratedAt.
// 2. Literates the sorted alerts, from the head,
func CombineAlertsByURL(url string) (*sdk.HealthChangeNotification, error) {
	alerts := []Alert{}
	if err := connection.Order("\"GeneratedAt\" asc").Where("\"URL\" = ?", url).Find(&alerts).Error; err != nil {
		log.WithFields(log.Fields{"url": url, "error": err}).Warn("[Notification-Repository] Combine alerts failed, get alerts by URL failed.")
		return nil, err
	}

	removeFromDB := []Alert{}
	// Save the record to the list for fast remove operation.
	l := list.New()
	for _, alert := range alerts {
		l.PushBack(alert)
	}

	// Pick up a element from the head. Literates to the end and remove the elements that the Key matahes key or Key matches VersusKey.
	for e := l.Front(); e != nil; e = e.Next() {
		alert := e.Value.(Alert)
		removeFromList := []*list.Element{}
		for r := e.Next(); r != nil; r = r.Next() {
			check := r.Value.(Alert)
			if check.Key == alert.VersusKey {
				removeFromList = append(removeFromList, r)
				removeFromDB = append(removeFromDB, check)
				continue
			}
			if check.Key == alert.Key {
				removeFromList = append(removeFromList, r)
				removeFromDB = append(removeFromDB, check)
				continue
			}
		}
		for _, toRemove := range removeFromList {
			l.Remove(toRemove)
		}
	}

	// Remove the records from DB.
	for _, toRemove := range removeFromDB {
		// Ignore errors here.
		// Errors may raise here, for example, another routine is doing the same work.
		// However, it seems OK. (Someone please help me to prove it)
		connection.Unscoped().Delete(&toRemove)
	}
	notification := sdk.HealthChangeNotification{}
	notification.URL = url
	for e := l.Front(); e != nil; e = e.Next() {
		alert := e.Value.(Alert)
		if alert.Severity == "Warning" {
			notification.Warnings++
		} else if alert.Severity == "Critical" {
			notification.Criticals++
		}
	}
	return &notification, nil
}