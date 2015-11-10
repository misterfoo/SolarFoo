package SolarFoo

import (
	"appengine"
	"appengine/mail"
	"appengine/urlfetch"
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

func init() {
	http.HandleFunc("/report", report)
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
	fmt.Fprintf(output, "<style type='text/css'>p { font-family: arial; }</style>")
	fmt.Fprint(output, "<p align='center'>")

	// Get the timestamp of midnight today, so we can calculate the energy used yesterday
	year, month, day := time.Now().Date()
	midnight := time.Date(year, month, day, 0, 0, 0, 0, time.Local)
	startTime := strconv.Itoa(int(midnight.Unix()))

	// Generate the usage report
	client := urlfetch.Client(c)
	resp, err := client.Get("http://egauge15255.egaug.es/cgi-bin/egauge-show?h&n=24&a&C&c&f=" + startTime)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Read all the points from eGauge and print the header
	totals, points := readValues(w, resp)
	fmt.Fprintf(output, "Used: %.2f kWh<br/>\n", totals.used)
	fmt.Fprintf(output, "Generated: %.2f kWh<br/>\n", totals.generated)
	fmt.Fprintf(output, "<a href='%v'>Full Report</a><br/>\n", bareUrl)
	fmt.Fprint(output, "<br/>")
	fmt.Fprint(output, "</p>")

	// Process the individual points for inclusion
	sort.Sort(ByTime(points))
	jsonPoints := new(bytes.Buffer)
	details := new(bytes.Buffer)
	for _, point := range points {
		fmt.Fprintf(details, "%v: %.2f, %.2f<br/>\n", point.time.Format("2006 Jan 2 15:04"), point.used, point.generated)
		fmt.Fprintf(jsonPoints, "[{v: [%v, 0, 0], f: '%v'}, %v, %v],\n",
			point.time.Hour(), point.time.Format("03:04"), point.used, point.generated)
	}

	// Generate the chart form of the results
	code := strings.Replace(chartCode, "$(points)", jsonPoints.String(), 1)
	fmt.Fprint(output, code)
	fmt.Fprint(output, "<br/>")

	// Write the full details.
	fmt.Fprint(output, "<p align='center'>")
	fmt.Fprint(output, "Details by hour:<br/>\n")
	fmt.Fprint(output, details.String())
	fmt.Fprint(output, "</p>")

	// Finish the output.
	fmt.Fprint(output, "</body></html>")

	final := output.String()

	// Where should the output go?
	if email {
		msg := &mail.Message{
			Sender:   "charles.nevill@gmail.com",
			To:       []string{"charles.nevill@gmail.com"},
			Subject:  "eGauge daily summary",
			Body:     "yar",
			HTMLBody: output.String(),
		}
		if err := mail.Send(c, msg); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

		fmt.Fprintf(w, "Wrote %v bytes of HTML to email", len(msg.HTMLBody))
	} else {
		// Write the content to the browser
		fmt.Fprint(w, final)
	}
}

// Reads the raw point data from the eGauge API into a set of DataPoint structures and a 'totals' structure
func readValues(w http.ResponseWriter, resp *http.Response) (DataPoint, []DataPoint) {

	var totals DataPoint
	points := make([]DataPoint, 0, 100)
	zone := time.FixedZone("Austin", -6 * 60 * 60)

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
		g, _ := strconv.ParseFloat(record[2], 32)

		points = append(points, DataPoint{time: timestamp, used: u, generated: g})
		totals.used += u
		totals.generated += g
	}

	return totals, points
}

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
        colors: ['#9575cd', '#33ac71'],
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
