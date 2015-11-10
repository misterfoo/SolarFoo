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
	time time.Time
	generated float64
	used float64
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
	
	w.Header().Set("Content-Type", "text/html")
	output := new(bytes.Buffer)
	fmt.Fprintf(output, "<style type='text/css'>p { font-family: arial; }</style>")
	fmt.Fprint(output, "<p>")

	// Get the timestamp of midnight today, so we can calculate the energy used yesterday
	year, month, day := time.Now().Date()
	midnight := time.Date( year, month, day, 0, 0, 0, 0, time.Local )
	startTime := strconv.Itoa( int(midnight.Unix()) )

	// Generate the usage report
	client := urlfetch.Client(c)
	resp, err := client.Get("http://egauge15255.egaug.es/cgi-bin/egauge-show?h&n=24&a&C&c&f=" + startTime)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Read all the points from the API query
	totals, points := readValues(w, resp)	
	sort.Sort(ByTime(points))
	for _, point := range points {
		fmt.Fprintf(output, "%v: %.2f, %.2f<br/>\n", point.time, point.used, point.generated)
	}

	// Dump the summary for the day
	fmt.Fprintf(output, "Used: %.2f<br/>\n", totals.used)
	fmt.Fprintf(output, "Generated: %.2f<br/>\n", totals.generated)

	// Generate the chart form of the results
	code := strings.Replace(chartCode, "$(points)", "foo", 1)	
	fmt.Fprint(output, code)

	fmt.Fprint(output, "</p>")

	final := output.String();

	// Where should the output go?
	if( email ) {
		msg := &mail.Message {
			Sender:  "charles.nevill@gmail.com",
			To:      []string{"charles.nevill@gmail.com"},
			Subject: "eGauge daily summary",
			Body:    "yar",
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

func readValues(w http.ResponseWriter, resp *http.Response) (DataPoint, []DataPoint) {

	var totals DataPoint
	points := make([]DataPoint, 0, 100)

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

		if( i == 0 ) {
			continue;
		}

		ts, _ := strconv.Atoi( record[0] )
		timestamp := time.Unix( int64(ts), 0 )

		u, _ := strconv.ParseFloat( record[1], 32 )
		g, _ := strconv.ParseFloat( record[2], 32 )

		points = append(points, DataPoint{ time: timestamp, used: u, generated: g })
		totals.used += u
		totals.generated += g
	}

	return totals, points
}

const chartCode = `
  <script type="text/javascript" src="https://www.google.com/jsapi"></script>
  <div id="chart_div"></div>
  <script language="">
google.load('visualization', '1', {packages: ['corechart', 'bar']});
google.setOnLoadCallback(drawColColors);

function drawColColors() {
      var data = new google.visualization.DataTable();
      data.addColumn('timeofday', 'Time of Day');
      data.addColumn('number', 'Motivation Level');
      data.addColumn('number', 'Energy Level');

      data.addRows([
        $(points)
      ]);

      var options = {
        title: 'Motivation and Energy Level Throughout the Day',
        colors: ['#9575cd', '#33ac71'],
        hAxis: {
          title: 'Time of Day',
          format: 'h:mm a',
          viewWindow: {
            min: [5, 30, 0],
            max: [17, 30, 0]
          }
        },
        vAxis: {
          title: 'Rating (scale of 1-10)'
        }
      };

      var chart = new google.visualization.ColumnChart(document.getElementById('chart_div'));
      chart.draw(data, options);
    }
</script>
`
