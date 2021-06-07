package client

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	spb "github.com/openconfig/gribi/v1/proto/service"
	"google.golang.org/protobuf/testing/protocmp"
)

func TestHandleParams(t *testing.T) {
	tests := []struct {
		desc      string
		inOpts    []Opt
		wantState *clientState
		wantErr   bool
	}{{
		desc:      "client with default parameters",
		inOpts:    nil,
		wantState: &clientState{},
	}, {
		desc: "ALL_PRIMARY client",
		inOpts: []Opt{
			AllPrimaryClients(),
		},
		wantState: &clientState{
			SessParams: &spb.SessionParameters{
				Redundancy: spb.SessionParameters_ALL_PRIMARY,
			},
		},
	}, {
		desc: "SINGLE_PRIMARY client",
		inOpts: []Opt{
			ElectedPrimaryClient(&spb.Uint128{High: 0, Low: 1}),
		},
		wantState: &clientState{
			SessParams: &spb.SessionParameters{
				Redundancy: spb.SessionParameters_SINGLE_PRIMARY,
			},
			ElectionID: &spb.Uint128{High: 0, Low: 1},
		},
	}, {
		desc: "SINGLE_PRIMARY and ALL_PRIMARY both included",
		inOpts: []Opt{
			ElectedPrimaryClient(&spb.Uint128{High: 0, Low: 1}),
			AllPrimaryClients(),
		},
		wantErr: true,
	}}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got, err := handleParams(tt.inOpts...)
			if (err != nil) != tt.wantErr {
				t.Fatalf("did not get expected error, wanted error? %v got error: %v", tt.wantErr, err)
			}
			if diff := cmp.Diff(tt.wantState, got, protocmp.Transform()); diff != "" {
				t.Fatalf("did not get expected state, diff(-want,+got):\n%s", diff)
			}
		})
	}
}

func TestQ(t *testing.T) {
	tests := []struct {
		desc      string
		inReqs    []*spb.ModifyRequest
		inSending bool
		wantQ     []*spb.ModifyRequest
	}{{
		desc: "single enqueued input",
		inReqs: []*spb.ModifyRequest{{
			ElectionId: &spb.Uint128{
				Low:  1,
				High: 1,
			},
		}},
		wantQ: []*spb.ModifyRequest{{
			ElectionId: &spb.Uint128{
				Low:  1,
				High: 1,
			},
		}},
	}, {
		desc: "multiple enqueued input",
		inReqs: []*spb.ModifyRequest{{
			ElectionId: &spb.Uint128{
				Low:  1,
				High: 1,
			},
		}, {
			ElectionId: &spb.Uint128{
				Low:  2,
				High: 2,
			},
		}},
		wantQ: []*spb.ModifyRequest{{
			ElectionId: &spb.Uint128{
				Low:  1,
				High: 1,
			},
		}, {
			ElectionId: &spb.Uint128{
				Low:  2,
				High: 2,
			},
		}},
	}, {
		desc: "enqueue whilst sending",
		inReqs: []*spb.ModifyRequest{{
			ElectionId: &spb.Uint128{
				Low:  1,
				High: 1,
			},
		}},
		inSending: true,
		wantQ:     []*spb.ModifyRequest{},
	}}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			c, err := New()
			if err != nil {
				t.Fatalf("cannot create client, %v", err)
			}
			c.qs.sending = tt.inSending
			for _, r := range tt.inReqs {
				c.Q(r)
			}
			if diff := cmp.Diff(c.qs.sendq, tt.wantQ, protocmp.Transform()); diff != "" {
				t.Fatalf("did not get expected send queue, %s", diff)
			}
		})
	}
}

func TestPending(t *testing.T) {
	tests := []struct {
		desc     string
		inClient *Client
		want     []PendingRequest
		wantErr  bool
	}{{
		desc: "empty queue",
		inClient: &Client{
			qs: &clientQs{
				pendq: &pendingQueue{},
			},
		},
		want: []PendingRequest{},
	}, {
		desc: "populated operations queue",
		inClient: &Client{
			qs: &clientQs{
				pendq: &pendingQueue{
					Ops: map[uint64]*PendingOp{
						1:  {Timestamp: 1, Op: &spb.AFTOperation{Id: 1}},
						42: {Timestamp: 42, Op: &spb.AFTOperation{Id: 42}},
						84: {Timestamp: 84, Op: &spb.AFTOperation{Id: 84}},
					},
				},
			},
		},
		want: []PendingRequest{
			&PendingOp{Timestamp: 1, Op: &spb.AFTOperation{Id: 1}},
			&PendingOp{Timestamp: 42, Op: &spb.AFTOperation{Id: 42}},
			&PendingOp{Timestamp: 84, Op: &spb.AFTOperation{Id: 84}},
		},
	}, {
		desc: "populated election ID",
		inClient: &Client{
			qs: &clientQs{
				pendq: &pendingQueue{
					Election: &ElectionReqDetails{
						Timestamp: 21,
						ID:        &spb.Uint128{High: 1, Low: 1},
					},
				},
			},
		},
		want: []PendingRequest{
			&ElectionReqDetails{
				Timestamp: 21,
				ID:        &spb.Uint128{High: 1, Low: 1},
			},
		},
	}, {
		desc: "populated session parameters",
		inClient: &Client{
			qs: &clientQs{
				pendq: &pendingQueue{
					SessionParams: &SessionParamReqDetails{
						Timestamp: 42,
						Outgoing: &spb.SessionParameters{
							AckType: spb.SessionParameters_RIB_AND_FIB_ACK,
						},
					},
				},
			},
		},
		want: []PendingRequest{
			&SessionParamReqDetails{
				Timestamp: 42,
				Outgoing: &spb.SessionParameters{
					AckType: spb.SessionParameters_RIB_AND_FIB_ACK,
				},
			},
		},
	}, {
		desc: "invalid operation in Ops queue",
		inClient: &Client{
			qs: &clientQs{
				pendq: &pendingQueue{
					Ops: map[uint64]*PendingOp{
						0: {Timestamp: 42},
					},
				},
			},
		},
		wantErr: true,
	}, {
		desc: "all queues populated",
		inClient: &Client{
			qs: &clientQs{
				pendq: &pendingQueue{
					Election:      &ElectionReqDetails{Timestamp: 0},
					SessionParams: &SessionParamReqDetails{Timestamp: 1},
					Ops: map[uint64]*PendingOp{
						0: {Timestamp: 3, Op: &spb.AFTOperation{Id: 42}},
					},
				},
			},
		},
		want: []PendingRequest{
			&PendingOp{Timestamp: 3, Op: &spb.AFTOperation{Id: 42}},
			&ElectionReqDetails{Timestamp: 0},
			&SessionParamReqDetails{Timestamp: 1},
		},
	}, {
		desc:     "nil queues",
		inClient: &Client{},
		wantErr:  true,
	}}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got, err := tt.inClient.Pending()
			if (err != nil) != tt.wantErr {
				t.Fatalf("did not get expected error, got: %v, wantErr? %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(got, tt.want, protocmp.Transform()); diff != "" {
				t.Fatalf("did not get expected queue, diff(-got,+want):\n%s", diff)
			}
		})
	}
}

func TestResults(t *testing.T) {
	tests := []struct {
		desc     string
		inClient *Client
		want     []*OpResult
		wantErr  bool
	}{{
		desc: "empty queue",
		inClient: &Client{
			qs: &clientQs{
				resultq: []*OpResult{},
			},
		},
		want: []*OpResult{},
	}, {
		desc: "populated queue",
		inClient: &Client{
			qs: &clientQs{
				resultq: []*OpResult{{
					CurrentServerElectionID: &spb.Uint128{
						Low:  0,
						High: 1,
					},
				}},
			},
		},
		want: []*OpResult{{
			CurrentServerElectionID: &spb.Uint128{
				Low:  0,
				High: 1,
			},
		}},
	}, {
		desc:     "nil queues",
		inClient: &Client{},
		wantErr:  true,
	}}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got, err := tt.inClient.Results()
			if (err != nil) != tt.wantErr {
				t.Fatalf("did not get expected error, got: %v, wantErr? %v", err, tt.wantErr)
			}
			if !cmp.Equal(got, tt.want, protocmp.Transform()) {
				t.Fatalf("did not get expected queue, got: %v, want: %v", got, tt.want)
			}
		})
	}
}

func TestStatus(t *testing.T) {
	// overload unixTS so that it always returns 42.
	unixTS = func() int64 { return 42 }

	tests := []struct {
		desc       string
		inClient   *Client
		wantStatus *ClientStatus
		wantErr    bool
	}{{
		desc: "empty queues",
		inClient: &Client{
			qs: &clientQs{
				pendq:   &pendingQueue{},
				resultq: []*OpResult{},
			},
		},
		wantStatus: &ClientStatus{
			Timestamp:           42,
			PendingTransactions: []PendingRequest{},
			Results:             []*OpResult{},
			SendErrs:            []error{},
			ReadErrs:            []error{},
		},
	}, {
		desc: "populated queues",
		inClient: &Client{
			qs: &clientQs{
				pendq: &pendingQueue{
					Ops: map[uint64]*PendingOp{
						0: {Timestamp: 1, Op: &spb.AFTOperation{Id: 0}},
					},
					Election:      &ElectionReqDetails{Timestamp: 2},
					SessionParams: &SessionParamReqDetails{Timestamp: 3},
				},
				resultq: []*OpResult{{
					Timestamp: 50,
				}},
			},
		},
		wantStatus: &ClientStatus{
			Timestamp: 42,
			PendingTransactions: []PendingRequest{
				&PendingOp{Timestamp: 1, Op: &spb.AFTOperation{Id: 0}},
				&ElectionReqDetails{Timestamp: 2},
				&SessionParamReqDetails{Timestamp: 3},
			},
			Results: []*OpResult{{
				Timestamp: 50,
			}},
			SendErrs: []error{},
			ReadErrs: []error{},
		},
	}, {
		desc:     "erroneous queues",
		inClient: &Client{},
		wantErr:  true,
	}}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got, err := tt.inClient.Status()
			if (err != nil) != tt.wantErr {
				t.Fatalf("did not get expected error, got: %v, wantErr? %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(got, tt.wantStatus,
				protocmp.Transform(),
				cmpopts.IgnoreFields(ClientStatus{}, "SendErrs", "ReadErrs"),
				cmpopts.EquateEmpty()); diff != "" {
				t.Fatalf("did not get expected status, diff(-got,+want):\n%s", diff)
			}
		})
	}
}

func TestHandleModifyResponse(t *testing.T) {
	unixTS = func() int64 { return 42 }
	tests := []struct {
		desc        string
		inClient    *Client
		inResponse  *spb.ModifyResponse
		wantResults []*OpResult
		wantErr     bool
	}{{
		desc:     "invalid combination of fields populated",
		inClient: &Client{},
		inResponse: &spb.ModifyResponse{
			Result: []*spb.AFTResult{{
				Id: 42,
			}},
			ElectionId: &spb.Uint128{High: 42, Low: 0},
		},
		wantErr: true,
	}, {
		desc: "election ID populated",
		inClient: &Client{
			qs: &clientQs{
				pendq: &pendingQueue{
					Election: &ElectionReqDetails{
						Timestamp: 2,
					},
				},
			},
		},
		inResponse: &spb.ModifyResponse{
			ElectionId: &spb.Uint128{High: 0, Low: 42},
		},
		wantResults: []*OpResult{{
			Timestamp:               42,
			Latency:                 40,
			CurrentServerElectionID: &spb.Uint128{High: 0, Low: 42},
		}},
	}, {
		desc: "invalid ModifyResponse",
		inClient: &Client{
			qs: &clientQs{
				pendq: &pendingQueue{},
			},
		},
		wantErr: true,
	}, {
		desc: "no populated election ID",
		inClient: &Client{
			qs: &clientQs{
				pendq: &pendingQueue{},
			},
		},
		inResponse: &spb.ModifyResponse{
			ElectionId: &spb.Uint128{Low: 1},
		},
		wantResults: []*OpResult{{
			Timestamp:               42,
			CurrentServerElectionID: &spb.Uint128{Low: 1},
			ClientError:             "received a election update when there was none pending",
		}},
	}, {
		desc: "session parameters populated",
		inClient: &Client{
			qs: &clientQs{
				pendq: &pendingQueue{
					SessionParams: &SessionParamReqDetails{
						Timestamp: 20,
					},
				},
			},
		},
		inResponse: &spb.ModifyResponse{
			SessionParamsResult: &spb.SessionParametersResult{
				Status: spb.SessionParametersResult_OK,
			},
		},
		wantResults: []*OpResult{{
			Timestamp: 42,
			Latency:   22,
			SessionParameters: &spb.SessionParametersResult{
				Status: spb.SessionParametersResult_OK,
			},
		}},
	}, {
		desc: "session parameters received but not pending",
		inClient: &Client{
			qs: &clientQs{
				pendq: &pendingQueue{},
			},
		},
		inResponse: &spb.ModifyResponse{
			SessionParamsResult: &spb.SessionParametersResult{
				Status: spb.SessionParametersResult_OK,
			},
		},
		wantResults: []*OpResult{{
			Timestamp: 42,
			SessionParameters: &spb.SessionParametersResult{
				Status: spb.SessionParametersResult_OK,
			},
			ClientError: "received a session parameter result when there was none pending",
		}},
	}}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			c := tt.inClient

			err := c.handleModifyResponse(tt.inResponse)
			if (err != nil) != tt.wantErr {
				t.Fatalf("did not get expected error status, got: %v, wantErr: %v?", err, tt.wantErr)
			}
			if err != nil {
				return
			}

			if diff := cmp.Diff(
				c.qs.resultq, tt.wantResults,
				protocmp.Transform(),
				cmpopts.EquateEmpty(),
				cmpopts.EquateErrors()); diff != "" {
				t.Fatalf("did not get expected result queue, diff(-got,+want):\n%s", diff)
			}
		})
	}
}

func TestHandleModifyRequest(t *testing.T) {
	// overload unix timestamp function to ensure output is deterministic.
	unixTS = func() int64 { return 42 }

	tests := []struct {
		desc        string
		inRequest   *spb.ModifyRequest
		inClient    *Client
		wantPending *pendingQueue
		wantErr     bool
	}{{
		desc: "valid input",
		inRequest: &spb.ModifyRequest{
			Operation: []*spb.AFTOperation{{
				Id: 1,
			}},
		},
		inClient: func() *Client { c, _ := New(); return c }(),
		wantPending: &pendingQueue{
			Ops: map[uint64]*PendingOp{
				1: {
					Timestamp: 42,
					Op:        &spb.AFTOperation{Id: 1},
				},
			},
		},
	}, {
		desc: "clashing transaction IDs",
		inRequest: &spb.ModifyRequest{
			Operation: []*spb.AFTOperation{{
				Id: 128,
			}},
		},
		inClient: &Client{
			qs: &clientQs{
				pendq: &pendingQueue{
					Ops: map[uint64]*PendingOp{
						128: {},
					},
				},
			},
		},
		wantPending: &pendingQueue{
			Ops: map[uint64]*PendingOp{
				128: {},
			},
		},
		wantErr: true,
	}, {
		desc: "election ID update",
		inClient: &Client{
			qs: &clientQs{
				pendq: &pendingQueue{},
			},
		},
		inRequest: &spb.ModifyRequest{ElectionId: &spb.Uint128{Low: 1}},
		wantPending: &pendingQueue{
			Election: &ElectionReqDetails{
				Timestamp: 42,
				ID:        &spb.Uint128{Low: 1},
			},
		},
	}, {
		desc: "session params update",
		inClient: &Client{
			qs: &clientQs{
				pendq: &pendingQueue{},
			},
		},
		inRequest: &spb.ModifyRequest{
			Params: &spb.SessionParameters{
				AckType: spb.SessionParameters_RIB_AND_FIB_ACK,
			},
		},
		wantPending: &pendingQueue{
			SessionParams: &SessionParamReqDetails{
				Timestamp: 42,
				Outgoing: &spb.SessionParameters{
					AckType: spb.SessionParameters_RIB_AND_FIB_ACK,
				},
			},
		},
	}}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if err := tt.inClient.handleModifyRequest(tt.inRequest); (err != nil) != tt.wantErr {
				t.Fatalf("did not get expected error, got: %v, wantErr? %v", err, tt.wantErr)
			}

			if diff := cmp.Diff(tt.inClient.qs.pendq, tt.wantPending, protocmp.Transform(), cmpopts.EquateEmpty()); diff != "" {
				t.Fatalf("did not get expected pending queue, diff(-got,+want):\n%s", diff)
			}
		})
	}
}

func TestConverged(t *testing.T) {
	tests := []struct {
		desc     string
		inClient *Client
		want     bool
	}{{
		desc: "converged - uninitialised",
		inClient: &Client{
			qs: &clientQs{},
		},
		want: true,
	}, {
		desc: "converged",
		inClient: &Client{
			qs: &clientQs{
				pendq: &pendingQueue{},
			},
		},
		want: true,
	}, {
		desc: "not converged - send queued",
		inClient: &Client{
			qs: &clientQs{
				sendq: []*spb.ModifyRequest{{}},
			},
		},
	}, {
		desc: "not converged - pending queue, ops",
		inClient: &Client{
			qs: &clientQs{
				pendq: &pendingQueue{
					Ops: map[uint64]*PendingOp{
						0: {},
					},
				},
			},
		},
	}, {
		desc: "not converged - pending queue, election",
		inClient: &Client{
			qs: &clientQs{
				pendq: &pendingQueue{
					Election: &ElectionReqDetails{},
				},
			},
		},
	}, {
		desc: "not converged - pending queue, params",
		inClient: &Client{
			qs: &clientQs{
				pendq: &pendingQueue{
					SessionParams: &SessionParamReqDetails{},
				},
			},
		},
	}}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got, want := tt.inClient.isConverged(), tt.want; got != want {
				t.Fatalf("did not get expected converged status, got: %v, want: %v", got, want)
			}
		})
	}

}