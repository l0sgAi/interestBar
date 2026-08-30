package application

import "testing"

func TestParseJudgeResult(t *testing.T) {
	cases := []struct {
		name       string
		input      string
		wantErr    bool
		wantReply  bool
		wantReason string
	}{
		{
			name:       "纯 JSON 通过",
			input:      `{"reply":true,"reason":"编程相关问题"}`,
			wantReply:  true,
			wantReason: "编程相关问题",
		},
		{
			name:       "纯 JSON 拒绝",
			input:      `{"reply":false,"reason":"与编程无关"}`,
			wantReply:  false,
			wantReason: "与编程无关",
		},
		{
			name:      "带 json fence",
			input:     "```json\n{\"reply\":true,\"reason\":\"命中条件\"}\n```",
			wantReply: true,
		},
		{
			name:      "带裸 fence",
			input:     "```\n{\"reply\":false,\"reason\":\"无关\"}\n```",
			wantReply: false,
		},
		{
			name:      "前后空白",
			input:     "  \n{\"reply\":true,\"reason\":\"ok\"}\n  ",
			wantReply: true,
		},
		{
			name:    "空输出",
			input:   "",
			wantErr: true,
		},
		{
			name:    "脏输出（解释性文字）",
			input:   "我认为应该回复，因为……",
			wantErr: true,
		},
		{
			name:      "非法 JSON 但含 reply 字段（兜底救回）",
			input:     `{"reply":true,`,
			wantReply: true,
		},
		{
			name:      "缺 reason 字段",
			input:     `{"reply":false}`,
			wantReply: false,
		},
		{
			name:      "半截 JSON 兜底 reply=true",
			input:     `{"reply":true,"reason":"编程相关问题，属于`,
			wantReply: true,
		},
		{
			name:      "半截 JSON 兜底 reply=false",
			input:     `{"reply":false,"reason":"闲聊与`,
			wantReply: false,
		},
		{
			name:    "半截 JSON 无 reply 字段",
			input:   `{"repl`,
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := parseJudgeResult(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expect error, got %+v", r)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if r.Reply != tc.wantReply {
				t.Fatalf("reply: want %v, got %v", tc.wantReply, r.Reply)
			}
			if tc.wantReason != "" && r.Reason != tc.wantReason {
				t.Fatalf("reason: want %q, got %q", tc.wantReason, r.Reason)
			}
		})
	}
}
