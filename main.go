package main

import (
	"context"
	"fmt"
	"html/template"
	"math/rand"
	"net/http"
	"os"
	"strconv"

	graphql "github.com/graph-gophers/graphql-go"
	"github.com/graph-gophers/graphql-go/relay"
	"github.com/graph-gophers/graphql-transport-ws/graphqlws"
	"github.com/mustafaturan/bus"
	"github.com/mustafaturan/monoton"
	"github.com/mustafaturan/monoton/sequencer"
)

const schema = `
	schema {
		subscription: Subscription
		mutation: Mutation
		query: Query
	}

	type Query {
		hello: String!
	}

	type Subscription {
		helloSaid(): HelloSaidEvent!
	}

	type Mutation {
		sayHello(msg: String!): HelloSaidEvent!
	}

	type HelloSaidEvent {
		id: String!
		msg: String!
	}
`

var httpPort = 8080

func init() {
	port := os.Getenv("HTTP_PORT")
	if port != "" {
		var err error
		httpPort, err = strconv.Atoi(port)
		if err != nil {
			panic(err)
		}
	}
}

func main() {
	// graphiql handler
	http.HandleFunc("/", http.HandlerFunc(graphiql))

	// init graphQL schema
	s, err := graphql.ParseSchema(schema, newResolver())
	if err != nil {
		panic(err)
	}

	// graphQL handler
	graphQLHandler := graphqlws.NewHandlerFunc(s, &relay.Handler{Schema: s})
	http.HandleFunc("/graphql", graphQLHandler)

	// start HTTP server
	if err := http.ListenAndServe(fmt.Sprintf(":%d", httpPort), nil); err != nil {
		panic(err)
	}
}

type resolver struct {
	bus *bus.Bus
	gen bus.Next
}

func newResolver() *resolver {
	m, _ := monoton.New(sequencer.NewMillisecond(), uint64(1), uint64(0))
	var gen bus.Next = m.Next

	bus, _ := bus.NewBus(gen)
	bus.RegisterTopics(helloEventName)

	r := &resolver{
		bus: bus,
		gen: gen,
	}

	return r
}

func (r *resolver) Hello() string {
	return "Hello world!"
}

func (r *resolver) SayHello(args struct{ Msg string }) *helloSaidEvent {
	e := &helloSaidEvent{msg: args.Msg, id: randomID()}
	go func() {
		r.bus.Emit(context.Background(), helloEventName, e)
	}()
	return e
}

func (r *resolver) HelloSaid(ctx context.Context) <-chan *helloSaidEvent {
	id := r.gen()
	c := make(chan *helloSaidEvent)

	handler := bus.Handler{
		Handle: func(e *bus.Event) {
			c <- e.Data.(*helloSaidEvent)
		},
		Matcher: helloEventName,
	}

	r.bus.RegisterHandler(id, &handler)

	callWhenDone(ctx, func() {
		r.bus.DeregisterHandler(id)
		close(c)
	})

	return c
}

func callWhenDone(ctx context.Context, f func()) {
	go func() {
		select {
		case <-ctx.Done():
			f()
			break
		}
	}()
}

const helloEventName string = "hello"

type helloSaidEvent struct {
	id  string
	msg string
}

func (r *helloSaidEvent) Msg() string {
	return r.msg
}

func (r *helloSaidEvent) ID() string {
	return r.id
}

func randomID() string {
	var letter = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

	b := make([]rune, 16)
	for i := range b {
		b[i] = letter[rand.Intn(len(letter))]
	}
	return string(b)
}

var graphiql = func(w http.ResponseWriter, r *http.Request) {
	t := template.Must(template.New("graphiql").Parse(`
  <!DOCTYPE html>
  <html>
       <head>
               <link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/graphiql/0.11.10/graphiql.css" />
               <script src="https://cdnjs.cloudflare.com/ajax/libs/fetch/1.1.0/fetch.min.js"></script>
               <script src="https://cdnjs.cloudflare.com/ajax/libs/react/15.5.4/react.min.js"></script>
               <script src="https://cdnjs.cloudflare.com/ajax/libs/react/15.5.4/react-dom.min.js"></script>
               <script src="https://cdnjs.cloudflare.com/ajax/libs/graphiql/0.11.10/graphiql.js"></script>
               <script src="//unpkg.com/subscriptions-transport-ws@0.8.3/browser/client.js"></script>
               <script src="//unpkg.com/graphiql-subscriptions-fetcher@0.0.2/browser/client.js"></script>
       </head>
       <body style="width: 100%; height: 100%; margin: 0; overflow: hidden;">
               <div id="graphiql" style="height: 100vh;">Loading...</div>
               <script>
                       function graphQLFetcher(graphQLParams) {
                               return fetch("/graphql", {
                                       method: "post",
                                       body: JSON.stringify(graphQLParams),
                                       credentials: "include",
                               }).then(function (response) {
                                       return response.text();
                               }).then(function (responseBody) {
                                       try {
                                               return JSON.parse(responseBody);
                                       } catch (error) {
                                               return responseBody;
                                       }
                               });
                       }

                       var subscriptionsClient = new window.SubscriptionsTransportWs.SubscriptionClient('ws://localhost:{{ . }}/graphql', { reconnect: true });
                       var subscriptionsFetcher = window.GraphiQLSubscriptionsFetcher.graphQLFetcher(subscriptionsClient, graphQLFetcher);

                       ReactDOM.render(
                               React.createElement(GraphiQL, {fetcher: subscriptionsFetcher}),
                               document.getElementById("graphiql")
                       );
               </script>
       </body>
  </html>
  `))
	t.Execute(w, httpPort)
}
