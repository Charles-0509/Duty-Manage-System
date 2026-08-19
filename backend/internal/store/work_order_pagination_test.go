package store

import (
	"fmt"
	"testing"

	"personnel-management-go/internal/types"
)

func TestListWorkOrdersPageReturnsCompleteJoinedOrders(t *testing.T) {
	appStore := newTestManagedStore(t)
	defer appStore.Close()
	if err := appStore.CreateSemesterMember(types.CreateMemberRequest{
		Username: "paged-member", RealName: "分页成员", Role: "USER", InitialPassword: "strong-member-password",
	}); err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= 25; index++ {
		if _, err := appStore.CreateWorkOrder(types.SaveWorkOrderRequest{
			Title:          fmt.Sprintf("分页工单%02d", index),
			BelongingMonth: "2026-08",
			WorkSessions: []types.WorkSession{
				{Date: "2026-08-10", WorkerName: "分页成员", Duration: 1},
				{Date: "2026-08-11", WorkerName: "分页成员", Duration: 2},
			},
		}, "系统管理员"); err != nil {
			t.Fatal(err)
		}
	}

	first, err := appStore.ListWorkOrdersPage("2026-08", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	last, err := appStore.ListWorkOrdersPage("2026-08", 3, 10)
	if err != nil {
		t.Fatal(err)
	}
	if first.Total != 25 || len(first.Items) != 10 || len(last.Items) != 5 {
		t.Fatalf("first=%+v last=%+v", first, last)
	}
	for _, order := range append(first.Items, last.Items...) {
		if len(order.WorkSessions) != 2 {
			t.Fatalf("order %s sessions=%d", order.ID, len(order.WorkSessions))
		}
	}
}
