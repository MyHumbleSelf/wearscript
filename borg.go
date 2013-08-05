package main

import (
	"fmt"
	"time"
	"github.com/garyburd/redigo/redis"
	picarus "github.com/bwhite/picarus/go"
	"code.google.com/p/go.net/websocket"
	"strings"
	"encoding/json"
)


type BorgSensor struct {
	Timestamp int64 `json:"timestamp"`	
	Accuracy  int `json:"accuracy"`
	Resolution  float64 `json:"resolution"`
	MaximumRange float64 `json:"maximumRange"`
	Type int `json:"type"`
	Name string `json:"name"`
	Values []float64 `json:"values"`
}

type BorgOptions struct {
	LocalImage bool `json:"localImage"`
	LocalSensors bool `json:"localSensors"`
	RemoteImage bool `json:"remoteImage"`
	RemoteSensors bool `json:"remoteSensors"`
	ImageDelay *float64 `json:"imageFrequency"`
	SensorDelay *float64 `json:"sensorFrequency"`
	Sensors []int `json:"sensors"`
}

type BorgData struct {
	Sensors []BorgSensor `json:"sensors"`
	Imageb64 *string `json:"imageb64"`
	Action string `json:"action"`
	Timestamp int64 `json:"timestamp"`
	TimestampAck int64 `json:"timestampAck"`
	H []float64 `json:"H"`
	Options *BorgOptions `json:"options"`
	Say *string `json:"say"`
}

func HMult(a, b []float64) []float64 {
	c := make([]float64, 9, 9)
	c[0] = a[0] * b[0] + a[1] * b[3] + a[2] * b[6]
	c[1] = a[0] * b[1] + a[1] * b[4] + a[2] * b[7]
	c[2] = a[0] * b[2] + a[1] * b[5] + a[2] * b[8]
	c[3] = a[3] * b[0] + a[4] * b[3] + a[5] * b[6]
	c[4] = a[3] * b[1] + a[4] * b[4] + a[5] * b[7]
	c[5] = a[3] * b[2] + a[4] * b[5] + a[5] * b[8]
	c[6] = a[6] * b[0] + a[7] * b[3] + a[8] * b[6]
	c[7] = a[6] * b[1] + a[7] * b[4] + a[8] * b[7]
	c[8] = a[6] * b[2] + a[7] * b[5] + a[8] * b[8]
	return c
}

func BorgGlassHandler(c *websocket.Conn) {
	defer c.Close()
    conn := picarus.Conn{Email: picarusEmail, ApiKey: picarusApiKey, Server: "https://api.picar.us"}
	path := strings.Split(c.Request().URL.Path, "/")
	if len(path) != 4 {
		fmt.Println("Bad path")
		return
	}
	userId, err := getSecretUser("borg", secretHash(path[len(path) - 1]))
	if err != nil {
		fmt.Println(err)
		return
	}
	flags, err := getUserFlags(userId, "uflags")
	if err != nil {
		fmt.Println(fmt.Errorf("Couldn't get flags: %s", err))
		return
	}
	// Send options
	go func() {
		fmt.Println("Sending options")
		// TODO: have it send this based on a subscription
		err = websocket.JSON.Send(c, BorgData{Action: "options", Options: &BorgOptions{LocalImage: hasFlag(flags, "borg_local_image"), LocalSensors: hasFlag(flags, "borg_local_sensors"), RemoteImage: hasFlag(flags, "borg_server_image") || hasFlag(flags, "borg_serverdisk_image") || hasFlag(flags, "borg_web_image"), RemoteSensors: hasFlag(flags, "borg_server_sensors") || hasFlag(flags, "borg_web_sensors")}})
		if err != nil {
			fmt.Println(err)
		}		
	}()
	go func() {
		matchMementoChan := make(chan *BorgData)
		requestChan := make(chan *BorgData)
		// Match memento loop
		go func() {
			locFeat := picarus.B64Dec(locationFeatureModel)
			_, columnss, err := getMementoDB(&conn, userId)
			if err != nil {
				fmt.Println(err)
				fmt.Println("Unable to perform matches, can't load db")
				return
			}
			for {
				request, ok := <-matchMementoChan
				if !ok {
					break
				}
				st := time.Now()
				points1, err := ImagePoints(picarus.B64Dec(*(*request).Imageb64))
				if err != nil {
					fmt.Println(err)
					continue
				}
				for _, columns := range columnss {
					_, err := ImagePointsMatch(columns[locFeat], points1)
					if err != nil {
						fmt.Println(err)
						continue
					}
					note := columns["meta:note"]
					go func() {
						err = websocket.JSON.Send(c, BorgData{Say: &note})
						if err != nil {
							fmt.Println(err)
						}
					}()
				}
				fmt.Println("Finished matching memento")
				fmt.Println(time.Now().Sub(st).Nanoseconds())
			}
		}()
		// Match AR loop
		go func() {
			for {
				request, ok := <-requestChan
				if !ok {
					break
				}
				points0, err := getUserAttribute(userId, "match_features")
				if err != nil {
					fmt.Println(err)
					continue
				}
				points1, err := ImagePoints(picarus.B64Dec(*(*request).Imageb64))
				if err != nil {
					fmt.Println(err)
					continue
				}
				h, err := ImagePointsMatch(points0, points1)
				if err != nil {
					fmt.Println("No match")
					fmt.Println(err)
					continue
				}
				// 
				fmt.Println("Match")
				err = websocket.JSON.Send(c, BorgData{H: h, Action: "warpH"})
				if err != nil {
					fmt.Println(err)
					continue
				}
				fmt.Println(h)
				hSmallToBig := []float64{3., 0., 304., 0., 3., 388., 0., 0., 1.}
				hBigToGlass := []float64{1.3960742363652061, -0.07945137930533697, -1104.2947209648783, 0.006275578662065556, 1.3523872016751255, -504.1266472917187, -1.9269902737e-05, -9.708578143e-05, 1.0}
				hFinal := HMult(HMult(h, hSmallToBig), hBigToGlass)
				fmt.Println(hFinal)
				image, err := getUserAttribute(userId, "match_overlay")
				if err != nil {
					fmt.Println(err)
					continue
				}
				imageWarped, err := WarpImage(image, hFinal, 360, 640)
				if err != nil {
					fmt.Println(err)
					continue
				}
				imageWarpedB64 := picarus.B64Enc(imageWarped)
				err = websocket.JSON.Send(c, BorgData{Imageb64: &imageWarpedB64, Action: "setOverlay"})
				if err != nil {
					fmt.Println(err)
				}				
				fmt.Println("Finished computing homography")
			}
		}()
		// Data from glass loop
		cnt := 0
		for {
			request := BorgData{}
			err := websocket.JSON.Receive(c, &request)
			if err != nil {
				fmt.Println(err)
				return
			}
			cnt += 1
			requestJS, err := json.Marshal(request)
			if err != nil {
				fmt.Println(err)
				return
			}
			fmt.Println(request.Action)
			if (request.Action == "image" && hasFlag(flags, "borg_web_image")) || (request.Action == "sensors" && hasFlag(flags, "borg_web_sensors")) {
				userPublish(userId, "borg_server_to_web", string(requestJS))
			}
			if request.Action == "image"  {
				if hasFlag(flags, "borg_serverdisk_image") {
					go func() {
						WriteFile(fmt.Sprintf("borg-serverdisk-%s-%.5d.jpg", userId, cnt), picarus.B64Dec(*request.Imageb64))
					}()
				}
				go func() {
					err = websocket.JSON.Send(c, BorgData{Action: "imageAck", TimestampAck: request.Timestamp})
					if err != nil {
						fmt.Println(err)
					}
				}()
				if hasFlag(flags, "match_annotated") {
					select {
					case requestChan <- &request:
					default:
						fmt.Println("Image skipping match, too slow...")
					}
				}
				if hasFlag(flags, "match_memento_borg") {
					select {
					case matchMementoChan <- &request:
					default:
						fmt.Println("Image skipping match memento, too slow...")
					}
				}
			}
		}
	}()
	psc, err := userSubscribe(userId, "borg_web_to_server")
	if err != nil {
		fmt.Println(err)
		return
	}
	// Data from web loop
	for {
		switch n := psc.Receive().(type) {
		case redis.Message:
			fmt.Printf("Message: %s\n", n.Channel)
			response := BorgData{}
			err := json.Unmarshal(n.Data, &response)
			if err != nil {
				fmt.Println(err)
				return
			}
			err = websocket.JSON.Send(c, response)
			if err != nil {
				return
			}
		case error:
			fmt.Printf("error: %v\n", n)
			return
		}
	}
}

func BorgWebHandler(c *websocket.Conn) {
	defer c.Close()
	userId := "219250584360_109113122718379096525"
	fmt.Println("Websocket connected")
	psc, err := userSubscribe(userId, "borg_server_to_web")
	if err != nil {
		fmt.Println(err)
		return
	}
	// Data from server loop
	go func() {
		responseChan := make(chan *BorgData)
		go func() {
			for {
				response, ok := <-responseChan
				if !ok {
					break
				}
				err = websocket.JSON.Send(c, response)
				if err != nil {
					fmt.Println(err)
					return
				}
			}
		}()
		for {
			switch n := psc.Receive().(type) {
			case redis.Message:
				response := BorgData{}
				err := json.Unmarshal(n.Data, &response)
				if err != nil {
					fmt.Println(err)
					return
				}
				select {
					case responseChan <- &response:
					default:
						fmt.Println("Image skipping sending to webapp, too slow...")
					}
			case error:
				fmt.Printf("error: %v\n", n)
				return
			}
		}
	}()
	// Data from web loop
	for {
		request := BorgData{}
		err := websocket.JSON.Receive(c, &request)
		if err != nil {
			fmt.Println(err)
			return
		}
		if request.Action == "setOverlay" {
			requestJS, err := json.Marshal(request)
			if err != nil {
				fmt.Println(err)
				return
			}
			userPublish(userId, "borg_web_to_server", string(requestJS))
		} else if request.Action == "setMatchOverlay" {
			setUserAttribute(userId, "match_overlay", picarus.B64Dec(*request.Imageb64))
		} else if request.Action == "setMatchImage" {
			points, err := ImagePoints(picarus.B64Dec(*request.Imageb64))
			if err != nil {
				fmt.Println(err)
				continue
			}
			setUserAttribute(userId, "match_features", points)
			fmt.Println("Finished setting match image")
		}
	}
}