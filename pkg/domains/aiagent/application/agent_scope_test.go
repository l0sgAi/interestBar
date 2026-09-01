// agent_scope_test.go 机器人 @提及 作用域（users.agent_circle_id 投影）写路径单元测试。
//
// 覆盖设计文档 P0-8 的服务层可测项：创建时圈内机器人写入圈子绑定 / 全局机器人不写绑定、
// 两条删除路径（全局 DeleteAgent / 圈内 DeleteCircleAgent）软删成功后清绑定、
// 清列失败/端口未注入 fail-open（不回滚删除结果）。
package application

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

var botLinkedUserID = uuid.MustParse("0192a0d0-0000-7000-8000-0000000000b1")

// newScopedAgentSvc 构造注入了 creator + scopeCleaner 的圈内 Service。
func newScopedAgentSvc(repo *fakeAgentRepo, roles *fakeCircleRoles, creator *fakeBotUserCreator, cleaner *fakeBotUserScopeCleaner) CircleAgentService {
	svc := NewCircleAgentService(repo)
	svc.SetCircleRoleReader(roles)
	svc.SetBotUserCreator(creator)
	svc.SetBotUserScopeCleaner(cleaner)
	return svc
}

// TestCreateCircleAgent_ScopesBotUser 圈内创建：CreateBotUser 收到本圈 circleID（投影列写入）。
func TestCreateCircleAgent_ScopesBotUser(t *testing.T) {
	repo := newFakeAgentRepo()
	creator := &fakeBotUserCreator{}
	svc := newScopedAgentSvc(repo, newFakeCircleRoles(), creator, &fakeBotUserScopeCleaner{})

	vo, err := svc.CreateCircleAgent(context.Background(), ownerID, circleAID, createInput())
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if creator.gotCircle == nil || *creator.gotCircle != circleAID {
		t.Fatalf("CreateBotUser circleID = %v, want circleAID（圈内机器人必须写 users.agent_circle_id）", creator.gotCircle)
	}
	if vo.LinkedUserID == uuid.Nil {
		t.Fatal("LinkedUserID not set")
	}
}

// TestCreateGlobalAgent_NotScoped 全局创建：CreateBotUser 收到 nil circleID（全站可见语义）。
func TestCreateGlobalAgent_NotScoped(t *testing.T) {
	repo := newFakeAgentRepo()
	creator := &fakeBotUserCreator{}
	svc := NewAgentService(repo)
	svc.SetRoleReader(&fakeUserRoleReader{role: 1, ok: true})
	svc.SetBotUserCreator(creator)

	if _, err := svc.CreateAgent(context.Background(), ownerID, createInput()); err != nil {
		t.Fatalf("global create failed: %v", err)
	}
	if creator.gotCircle != nil {
		t.Fatalf("CreateBotUser circleID = %v, want nil（全局机器人不得写绑定列）", *creator.gotCircle)
	}
}

// TestDeleteAgent_ClearsScope 全局删除：软删成功后清 linkedUserID 的圈子绑定。
func TestDeleteAgent_ClearsScope(t *testing.T) {
	repo := newFakeAgentRepo()
	agent := seedGlobalAgent(repo)
	agent.LinkedUserID = botLinkedUserID

	cleaner := &fakeBotUserScopeCleaner{}
	svc := NewAgentService(repo)
	svc.SetRoleReader(&fakeUserRoleReader{role: 1, ok: true})
	svc.SetBotUserScopeCleaner(cleaner)

	if err := svc.DeleteAgent(context.Background(), ownerID, globalAgentID); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != globalAgentID {
		t.Fatalf("SoftDelete not applied: %v", repo.deleted)
	}
	if len(cleaner.cleared) != 1 || cleaner.cleared[0] != botLinkedUserID {
		t.Fatalf("cleared = %v, want [botLinkedUserID]", cleaner.cleared)
	}
}

// TestDeleteCircleAgent_ClearsScope 圈内删除：软删成功后清 linkedUserID 的圈子绑定。
func TestDeleteCircleAgent_ClearsScope(t *testing.T) {
	repo := newFakeAgentRepo()
	agent := seedCircleAgent(repo)
	agent.LinkedUserID = botLinkedUserID

	cleaner := &fakeBotUserScopeCleaner{}
	svc := newScopedAgentSvc(repo, newFakeCircleRoles(), &fakeBotUserCreator{}, cleaner)

	if err := svc.DeleteCircleAgent(context.Background(), ownerID, circleAgentID); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != circleAgentID {
		t.Fatalf("SoftDelete not applied: %v", repo.deleted)
	}
	if len(cleaner.cleared) != 1 || cleaner.cleared[0] != botLinkedUserID {
		t.Fatalf("cleared = %v, want [botLinkedUserID]", cleaner.cleared)
	}
}

// TestDeleteCircleAgent_FailOpenOnCleanerError 清列失败不回滚删除（软删已生效，
// 列未清可幂等补偿）：删除仍返回 nil，且 SoftDelete 已记录。
func TestDeleteCircleAgent_FailOpenOnCleanerError(t *testing.T) {
	repo := newFakeAgentRepo()
	agent := seedCircleAgent(repo)
	agent.LinkedUserID = botLinkedUserID

	cleaner := &fakeBotUserScopeCleaner{clearErr: errors.New("redis down")}
	svc := newScopedAgentSvc(repo, newFakeCircleRoles(), &fakeBotUserCreator{}, cleaner)

	if err := svc.DeleteCircleAgent(context.Background(), ownerID, circleAgentID); err != nil {
		t.Fatalf("delete must succeed even when clearing fails, got: %v", err)
	}
	if len(repo.deleted) != 1 {
		t.Fatalf("SoftDelete not applied: %v", repo.deleted)
	}
}

// TestDeleteCircleAgent_FailOpen_NoCleaner 端口未注入（装配遗漏）：跳过清列不 panic，
// 删除主流程照常成功（与 botUserUpdater 的 nil 处理范式一致）。
func TestDeleteCircleAgent_FailOpen_NoCleaner(t *testing.T) {
	repo := newFakeAgentRepo()
	agent := seedCircleAgent(repo)
	agent.LinkedUserID = botLinkedUserID

	svc := newCircleAgentSvc(repo, newFakeCircleRoles()) // 未 SetBotUserScopeCleaner
	if err := svc.DeleteCircleAgent(context.Background(), ownerID, circleAgentID); err != nil {
		t.Fatalf("delete failed without cleaner injected: %v", err)
	}
	if len(repo.deleted) != 1 {
		t.Fatalf("SoftDelete not applied: %v", repo.deleted)
	}
}
