package SolarFoo

import (
	"appengine"
	"appengine/urlfetch"
    "github.com/sendgrid/sendgrid-go"
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"keys" // this is a local package not stored in Git
)

func init() {
	http.HandleFunc("/report", report)
	http.HandleFunc("/emailTest", emailTest)
}

type DataPoint struct {
	time      time.Time
	generated float64
	used      float64
}

// ByTime implements sort.Interface for []DataPoint based on the 'time' field.
type ByTime []DataPoint

func (a ByTime) Len() int           { return len(a) }
func (a ByTime) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByTime) Less(i, j int) bool { return a[i].time.Before(a[j].time) }

// Tests email functionality
func emailTest(w http.ResponseWriter, r *http.Request) {
	ctx := appengine.NewContext(r)
    sg := sendgrid.NewSendGridClientWithApiKey(keys.SendGridApi)
    sg.Client = urlfetch.Client(ctx)

    message := sendgrid.NewMail()
    message.AddTo("charles.nevill@gmail.com")
    message.SetSubject("Email From SendGrid")
    message.SetHTML("Through AppEngine")
    message.SetFrom("charles.nevill@gmail.com")
    if err := sg.Send(message); err == nil {
		fmt.Fprint(w, "yup!")
	} else {
		fmt.Fprintf(w, "sad panda: %v", err)
	}
}

func report(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)

	// Should we send emails?
	email := false
	if _, ok := r.URL.Query()["email"]; ok {
		email = true
	}
	
	bareUrl := r.URL
	bareUrl.RawQuery = "" 

	// Setup the page header
	w.Header().Set("Content-Type", "text/html")
	output := new(bytes.Buffer)
	fmt.Fprint(output, "<html><body>")
	fmt.Fprintf(output, "<style type='text/css'>%v</style>", pageStyle)
	fmt.Fprint(output, "<p align='center'>")

	// Figure out the desired time zone for presenting results.
	zone, err := time.LoadLocation("America/Chicago")
	if err != nil {
		zone = time.FixedZone("Austin", -6 * 60 * 60)
	}

	// Get the timestamp of the beginning of today, so we can calculate the energy used yesterday.
	// This will be passed as the "first" value to the eGauge API, but since the API returns
	// rows in reverse chronological order (newest to oldest), we'll get the data from a 24
	// hour period *ending* at this time.
	year, month, day := time.Now().In(zone).Date()
	startOfToday := time.Date(year, month, day, 0, 0, 0, 0, zone)
	firstRowToQuery := strconv.Itoa(int(startOfToday.Unix()))

	// Compute the start of the actual day we're reporting on.
	oneDay, _ := time.ParseDuration("-24h");
	actualDayOfReport := startOfToday.Add(oneDay)

	// Generate the usage report
	client := urlfetch.Client(c)
	resp, err := client.Get("http://egauge15255.egaug.es/cgi-bin/egauge-show?h&n=24&a&C&c&f=" + firstRowToQuery)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Read all the points from eGauge and print the header
	totals, points := readValues(w, resp, zone)
	fmt.Fprintf(output, "Report for: %v<br/>\n", actualDayOfReport.Format("2006 Jan 2"))
	fmt.Fprintf(output, "Used: %.2f kWh<br/>\n", totals.used)
	fmt.Fprintf(output, "Generated: %.2f kWh<br/>\n", totals.generated)
	fmt.Fprint(output, "<br/>")
	fmt.Fprint(output, "</p>")

	// Process the individual points for inclusion
	sort.Sort(ByTime(points))
	jsonPoints := new(bytes.Buffer)
	details := new(bytes.Buffer)
	for _, point := range points {
		// From white to full red
		usedColor := 255 - (math.Min( point.used / 5, 1 ) * 255)
		usedColorCss := fmt.Sprintf("#ff%2x%2x", int(usedColor), int(usedColor))

		// From white to full green
		genColor := 255 - (math.Min( point.generated / 5, 1 ) * 255)
		genColorCss := fmt.Sprintf("#%2xff%2x", int(genColor), int(genColor))

		fmt.Fprintf(details, "<tr><td>%v</td><td bgcolor='%v'>%.2f kWh</td><td bgcolor='%v'>%.2f kWh</td></tr>\n",
			point.time.Format("15:04"), usedColorCss, point.used, genColorCss, point.generated)
		fmt.Fprintf(jsonPoints, "[{v: [%v, 0, 0], f: '%v'}, %v, %v],\n",
			point.time.Hour(), point.time.Format("03:04"), point.used, point.generated)
	}

	// Generate the chart form of the results
	code := strings.Replace(chartCode, "$(points)", jsonPoints.String(), 1)
	fmt.Fprint(output, code)
	fmt.Fprint(output, "<br/>")

	// Write the full details.
	fmt.Fprint(output, "<p align='center'>")
	fmt.Fprint(output, "Details<br/>\n")
	fmt.Fprint(output, "<center>\n")
	fmt.Fprint(output, "<table><tr><th class='details'>Time</th><th class='details'>Used</th><th class='details'>Generated</th></tr>\n")
	fmt.Fprint(output, details.String())
	fmt.Fprint(output, "</table>\n")
	fmt.Fprint(output, "</center>\n")

	// Finish the output.
	fmt.Fprint(output, "</body></html>")

	final := output.String()

	// Where should the output go?
	if email {
		sg := sendgrid.NewSendGridClientWithApiKey(keys.SendGridApi)
		sg.Client = urlfetch.Client(c)

		body := output.String()
		msg := sendgrid.NewMail()
		msg.SetFrom("charles.nevill@gmail.com")
		msg.AddTo("charles.nevill@gmail.com")
		msg.SetSubject("eGauge daily summary")
		msg.SetHTML(body)
		if err := sg.Send(msg); err == nil {
			fmt.Fprintf(w, "Wrote %v bytes HTML email:", len(body))
	        fmt.Fprint(w, body)
		} else {
			fmt.Fprintf(w, "sad panda: %v", err)
		}
	} else {
		// Write the content to the browser
		fmt.Fprint(w, final)
	}
}

// Reads the raw point data from the eGauge API into a set of DataPoint structures and a 'totals' structure
func readValues(w http.ResponseWriter, resp *http.Response, zone *time.Location) (DataPoint, []DataPoint) {

	var totals DataPoint
	points := make([]DataPoint, 0, 100)

	// Read all of the points we got from the server.
	reader := csv.NewReader(resp.Body)
	reader.FieldsPerRecord = 6
	for i := 0; ; i++ {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			break
		}

		if i == 0 {
			continue
		}

		ts, _ := strconv.Atoi(record[0])
		timestamp := time.Unix(int64(ts), 0).In(zone)

		u, _ := strconv.ParseFloat(record[1], 32)
		u = math.Max(u, 0)
		g, _ := strconv.ParseFloat(record[2], 32)
		g = math.Max(g, 0)

		points = append(points, DataPoint{time: timestamp, used: u, generated: g})
		totals.used += u
		totals.generated += g
	}

	return totals, points
}

const pageStyle = `
	p, center {
		font-family: arial;
	}

	table, th, td {
		text-align: center;
	}

	th.details {
		width: 110;
	}
`

const chartCode = `
  <script type="text/javascript" src="https://www.google.com/jsapi"></script>
  <div id="chart_div"></div>
  <script type='text/javascript'>
//<![CDATA[
google.load('visualization', '1', {packages: ['corechart', 'bar']});
google.setOnLoadCallback(drawColColors);

function drawColColors() {
      var data = new google.visualization.DataTable();
      data.addColumn('timeofday', 'Time of Day');
      data.addColumn('number', 'Used');
      data.addColumn('number', 'Generated');

      data.addRows([
        $(points)
      ]);

      var options = {
        title: 'Usage and Solar Generation',
        colors: ['#ee0000', '#00dd00'],
		chartArea: {width:'80%',height:100},
        hAxis: {
          title: 'Time of Day',
          format: 'h:mm a',
          viewWindow: {
            min: [0, 0, 0],
            max: [23, 59, 0]
          }
        },
        vAxis: {
          title: 'kWh',
		  viewWindow: {
			  min: 0
		  }
        }
      };

      var chart = new google.visualization.ColumnChart(document.getElementById('chart_div'));
      chart.draw(data, options);
    }
//]]> 
</script>
`
