// Copyright 2017 Vector Creations Ltd
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package input contains the code processes new room events
package input

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/matrix-org/dendrite/common"
	"github.com/matrix-org/dendrite/roomserver/api"
	"github.com/matrix-org/util"
	sarama "gopkg.in/Shopify/sarama.v1"
)

// RoomserverInputAPI implements api.RoomserverInputAPI
type RoomserverInputAPI struct {
	DB       RoomEventDatabase
	Producer sarama.SyncProducer
	// The kafkaesque topic to output new room events to.
	// This is the name used in kafka to identify the stream to write events to.
	OutputRoomEventTopic string
	// Protects calls to processRoomEvent
	mutex sync.Mutex
}

// WriteOutputEvents implements OutputRoomEventWriter
func (r *RoomserverInputAPI) WriteOutputEvents(roomID string, updates []api.OutputEvent) error {
	messages := make([]*sarama.ProducerMessage, len(updates))
	for i := range updates {
		value, err := json.Marshal(updates[i])
		if err != nil {
			return err
		}
		messages[i] = &sarama.ProducerMessage{
			Topic: r.OutputRoomEventTopic,
			Key:   sarama.StringEncoder(roomID),
			Value: sarama.ByteEncoder(value),
		}
	}
	return r.Producer.SendMessages(messages)
}

// InputRoomEvents implements api.RoomserverInputAPI
func (r *RoomserverInputAPI) InputRoomEvents(
	ctx context.Context,
	request *api.InputRoomEventsRequest,
	response *api.InputRoomEventsResponse,
) error {
	// We lock as processRoomEvent can only be called once at a time
	r.mutex.Lock()
	defer r.mutex.Unlock()
	for i := range request.InputRoomEvents {
		if err := processRoomEvent(ctx, r.DB, r, request.InputRoomEvents[i]); err != nil {
			util.GetLogger(ctx).Warnf("ProcessRoomEvent returned %s", err)
			return err
		}
		util.GetLogger(ctx).Infof("ProcessRoomEvent done (%i)", i)
	}
	util.GetLogger(ctx).Infof("ProcessRoomEvent done done")
	for i := range request.InputInviteEvents {
		if err := processInviteEvent(ctx, r.DB, r, request.InputInviteEvents[i]); err != nil {
			return err
		}
	}
	util.GetLogger(ctx).Info("InputRoomEvents done")
	return nil
}

// SetupHTTP adds the RoomserverInputAPI handlers to the http.ServeMux.
func (r *RoomserverInputAPI) SetupHTTP(servMux *http.ServeMux) {
	servMux.Handle(api.RoomserverInputRoomEventsPath,
		common.MakeInternalAPI("inputRoomEvents", func(req *http.Request) util.JSONResponse {
			var request api.InputRoomEventsRequest
			var response api.InputRoomEventsResponse
			if err := json.NewDecoder(req.Body).Decode(&request); err != nil {
				return util.MessageResponse(400, err.Error())
			}
			if err := r.InputRoomEvents(req.Context(), &request, &response); err != nil {
				return util.ErrorResponse(err)
			}
			return util.JSONResponse{Code: 200, JSON: &response}
		}),
	)
}
