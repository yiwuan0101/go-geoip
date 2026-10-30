package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/oschwald/maxminddb-golang"
	"go-geoip/common"
	"go-geoip/common/config"
	logger "go-geoip/common/loggger"
	"go-geoip/middleware"
	"go-geoip/router"
)

const (
	cityDBDefaultURL = "https://github.com/P3TERX/GeoLite.mmdb/raw/download/GeoLite2-City.mmdb"
	asnDBURL         = "https://github.com/P3TERX/GeoLite.mmdb/raw/download/GeoLite2-ASN.mmdb"
	cnDBURL          = "https://github.com/ljxi/GeoCN/releases/latest/download/GeoCN.mmdb"
	sessionName      = "session"
)

// httpClient leaves enough headroom for ~60MB databases while still
// guarding against a hung upstream.
var httpClient = &http.Client{Timeout: 10 * time.Minute}

type database struct {
	filename string
	url      string
}

func databases() []database {
	return []database{
		{filename: "GeoIP-City.mmdb", url: getCityDBURL()},
		{filename: "Geo-ASN.mmdb", url: getAsnDBURL()},
		{filename: "GeoCN.mmdb", url: getCnDBURL()},
	}
}

func main() {
	logger.SetupLogger()
	logger.SysLog(fmt.Sprintf("go-geoip %s started", common.Version))

	setGinMode()
	logDebugMode()

	// Load databases synchronously so the server never accepts requests
	// before the readers are ready.
	if err := loadDatabases(); err != nil {
		logger.FatalLog("initial database load failed: %v", err)
	}

	server := setupServer()

	go scheduleWeeklyUpdate()

	runServer(server)
}

func setGinMode() {
	if os.Getenv("GIN_MODE") != "debug" {
		gin.SetMode(gin.ReleaseMode)
	}
}

func logDebugMode() {
	if config.DebugEnabled {
		logger.SysLog("running in debug mode")
	}
}

func setupServer() *gin.Engine {
	server := gin.New()
	server.Use(gin.Recovery(), middleware.RequestId())
	middleware.SetUpLogger(server)

	store := cookie.NewStore([]byte(config.SessionSecret))
	server.Use(sessions.Sessions(sessionName, store))

	router.SetRouter(server)
	return server
}

func runServer(server *gin.Engine) {
	port := getPort()
	if err := server.Run(":" + port); err != nil {
		logger.FatalLog("failed to start HTTP server: %v", err)
	}
}

func getPort() string {
	if port := os.Getenv("PORT"); port != "" {
		return port
	}
	return strconv.Itoa(*common.Port)
}

func getCityDBURL() string {
	if config.CityDBRemoteUrl != "" {
		return config.CityDBRemoteUrl
	}
	return cityDBDefaultURL
}

func getAsnDBURL() string {
	if config.AsnDBRemoteUrl != "" {
		return config.AsnDBRemoteUrl
	}
	return asnDBURL
}

func getCnDBURL() string {
	if config.CnDBRemoteUrl != "" {
		return config.CnDBRemoteUrl
	}
	return cnDBURL
}

// loadDatabases prepares the three mmdb files on first start. Existing
// files that open as valid databases are reused as-is.
func loadDatabases() error {
	for _, db := range databases() {
		if isValidDatabase(db.filename) {
			continue
		}
		logger.SysLog(fmt.Sprintf("Downloading %s from %s...", db.filename, db.url))
		if err := downloadAndSave(db.filename, db.url); err != nil {
			return fmt.Errorf("prepare %s: %w", db.filename, err)
		}
		logger.SysLog(fmt.Sprintf("Downloaded and saved %s successfully", db.filename))
	}
	return openDatabases()
}

// refreshDatabases re-downloads every database but keeps the previous
// files on failure, so a broken upstream never takes the service down.
func refreshDatabases() error {
	var errs []string
	updated := false
	for _, db := range databases() {
		if err := downloadAndSave(db.filename, db.url); err != nil {
			errs = append(errs, err.Error())
			continue
		}
		logger.SysLog(fmt.Sprintf("Updated %s successfully", db.filename))
		updated = true
	}
	if !updated {
		return fmt.Errorf("all database downloads failed: %s", strings.Join(errs, "; "))
	}
	if err := openDatabases(); err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// isValidDatabase reports whether the file exists and opens as a valid
// mmdb database. Files corrupted by earlier failed downloads (e.g. a
// saved 404 page) fail this check and get re-downloaded.
func isValidDatabase(filename string) bool {
	if _, err := os.Stat(filename); err != nil {
		return false
	}
	r, err := maxminddb.Open(filename)
	if err != nil {
		return false
	}
	r.Close()
	return true
}

// downloadAndSave fetches url into filename via a temp file and replaces
// the target only after the payload verifies as a valid mmdb file.
func downloadAndSave(filename, url string) error {
	resp, err := httpClient.Get(url)
	if err != nil {
		return fmt.Errorf("download %s: %w", filename, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: unexpected HTTP status %d", filename, resp.StatusCode)
	}

	tmp := filename + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		os.Remove(tmp)
		return fmt.Errorf("save %s: %w", filename, err)
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("save %s: %w", filename, err)
	}
	if r, err := maxminddb.Open(tmp); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("%s is not a valid MaxMind database: %w", filename, err)
	} else {
		r.Close()
	}
	if err := os.Rename(tmp, filename); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("replace %s: %w", filename, err)
	}
	return nil
}

// openDatabases swaps all three readers atomically; on any failure the
// currently serving readers are left untouched.
func openDatabases() error {
	city, err := maxminddb.Open("GeoIP-City.mmdb")
	if err != nil {
		return fmt.Errorf("open city database: %w", err)
	}
	asn, err := maxminddb.Open("Geo-ASN.mmdb")
	if err != nil {
		city.Close()
		return fmt.Errorf("open asn database: %w", err)
	}
	cn, err := maxminddb.Open("GeoCN.mmdb")
	if err != nil {
		city.Close()
		asn.Close()
		return fmt.Errorf("open cn database: %w", err)
	}

	common.Mu.Lock()
	defer common.Mu.Unlock()
	for _, old := range []*maxminddb.Reader{common.CityReader, common.AsnReader, common.CnReader} {
		if old != nil {
			old.Close()
		}
	}
	common.CityReader, common.AsnReader, common.CnReader = city, asn, cn
	return nil
}

func scheduleWeeklyUpdate() {
	for {
		nextUpdateTime := getNextSundayLastSecond()
		durationUntilUpdate := time.Until(nextUpdateTime)
		logger.SysLog(fmt.Sprintf("Next database update scheduled at %s, which is in %v.", nextUpdateTime, durationUntilUpdate))

		timer := time.NewTimer(durationUntilUpdate)
		<-timer.C

		logger.SysLog("Updating databases...")
		if err := refreshDatabases(); err != nil {
			logger.SysLog(fmt.Sprintf("Database update failed, keeping old databases: %v", err))
		}
	}
}

func getNextSundayLastSecond() time.Time {
	now := time.Now()
	daysUntilSunday := (7 - int(now.Weekday())) % 7
	if daysUntilSunday == 0 && now.Hour() >= 0 {
		daysUntilSunday = 7
	}
	return time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, now.Location()).Add(time.Duration(daysUntilSunday) * 24 * time.Hour)
}
