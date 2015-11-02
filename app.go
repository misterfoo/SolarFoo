package SolarFoo

import (
	"appengine"
	"appengine/mail"
	"appengine/urlfetch"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

func init() {
	http.HandleFunc("/report", report)
}

func report(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	client := urlfetch.Client(c)
	
	w.Header().Set("Content-Type", "text/plain")

	// Get the timestamp of midnight today, so we can calculate the energy used yesterday
	year, month, day := time.Now().Date()
	midnight := time.Date( year, month, day, 0, 0, 0, 0, time.Local )
	startTime := strconv.Itoa( int(midnight.Unix()) )

	// Generate the usage report
	resp, err := client.Get("http://egauge15255.egaug.es/cgi-bin/egauge-show?h&n=24&a&C&c&f=" + startTime)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Read the results	
	reader := csv.NewReader( resp.Body )
	reader.FieldsPerRecord = 6
	used := 0.0
	generated := 0.0
	for i := 0; ; i++ {
 		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if( i == 0 ) {
			continue;
		}

		// Dump the timestamps for debugging purposes
		ts, _ := strconv.Atoi( record[0] )
		timestamp := time.Unix( int64(ts), 0 )
		fmt.Fprintf(w, "Timestamp: %v (%v)\n", timestamp, ts)

		u, _ := strconv.ParseFloat( record[1], 32 )
		g, _ := strconv.ParseFloat( record[2], 32 )

		used += u
		generated += g
	}

	fmt.Fprintf(w, "Used: %v\n", used)
	fmt.Fprintf(w, "Generated: %v\n", generated)

	// Send the email report
	msg := &mail.Message {
		Sender:  "charles.nevill@gmail.com",
		To:      []string{"charles.nevill@gmail.com"},
		Subject: "eGauge daily summary",
		Body:    fmt.Sprintf(reportMessage, used, generated),
	}
	if err := mail.Send(c, msg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	fmt.Fprintf(w, "Sent notification email too!\n\n%v\n", msg.Body)
}

const reportMessage = `
eGauge usage for yesterday:
Used: %.2f kWh
Generated: %.2f kWh
`
