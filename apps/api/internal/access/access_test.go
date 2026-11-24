package access

import (
	"errors"
	"slices"
	"testing"
)

func TestCan(t *testing.T) {
	var g Grants
	g.Add("", ContributionRead)
	g.Add("p1", ContributionCreate, ProjectCreate) // a system action on a project grants nothing
	cases := []struct {
		a    Action
		p    string
		want bool
	}{
		{ContributionRead, "p9", true},
		{ContributionCreate, "p1", true},
		{ContributionCreate, "p2", false},
		{ProjectCreate, "", false},
		{ProjectCreate, "p1", false},
	}
	for _, c := range cases {
		if got := g.Can(c.a, c.p); got != c.want {
			t.Errorf("Can(%s, %q) = %v, want %v", c.a, c.p, got, c.want)
		}
	}
}

func TestAdminCanEverything(t *testing.T) {
	var g Grants
	g.Add("", All)
	if !g.IsAdmin() {
		t.Fatal("IsAdmin() = false")
	}
	for _, a := range Actions {
		if !g.Can(a, "") || !g.Can(a, "px") {
			t.Fatalf("admin cannot %s", a)
		}
	}
}

func TestStarOnProjectIsNotAdmin(t *testing.T) {
	var g Grants
	g.Add("p1", All)
	if g.IsAdmin() || g.Can(ContributionRead, "p1") {
		t.Fatal("* on a project must grant nothing")
	}
}

func TestZeroGrantsCanNothing(t *testing.T) {
	var g Grants
	if g.Can(ContributionRead, "p") || g.IsAdmin() || g.ManagesAny() {
		t.Fatal("zero Grants granted something")
	}
	if all, ids := g.Readable(); all || len(ids) != 0 {
		t.Fatal(all, ids)
	}
}

func TestReadable(t *testing.T) {
	var g Grants
	g.Add("b", ContributionRead)
	g.Add("a", ContributionRead)
	g.Add("c", SignalCreate)
	all, ids := g.Readable()
	if all || !slices.Equal(ids, []string{"a", "b"}) {
		t.Fatal(all, ids)
	}
	g.Add("", ContributionRead)
	if all, _ := g.Readable(); !all {
		t.Fatal("read on * is not all")
	}
}

func TestManagesAny(t *testing.T) {
	var g Grants
	g.Add("p1", MemberManage)
	if !g.ManagesAny() {
		t.Fatal("ManagesAny() = false")
	}
}

func TestCanAssign(t *testing.T) {
	var mgr Grants
	mgr.Add("p1", ContributionRead, ContributionCreate, SignalCreate, MemberManage)
	viewer := []Action{ContributionRead}
	if !mgr.CanAssign(viewer, "p1") {
		t.Error("manager cannot assign viewer on own project")
	}
	if mgr.CanAssign(viewer, "p2") {
		t.Error("manager assigned on another project")
	}
	if mgr.CanAssign(viewer, "") {
		t.Error("manager assigned on *")
	}
	if mgr.CanAssign([]Action{All}, "p1") {
		t.Error("manager assigned admin")
	}
	if mgr.CanAssign([]Action{ContributionRead, ProjectCreate}, "p1") {
		t.Error("manager assigned a system action")
	}
	var star Grants
	star.Add("", MemberManage, ContributionRead)
	if !star.CanAssign(viewer, "") || !star.CanAssign(viewer, "p7") {
		t.Error("manager on * cannot assign")
	}
	var mixed Grants // member:Manage on *, read only on p1: cannot hand out read on *
	mixed.Add("", MemberManage)
	mixed.Add("p1", ContributionRead)
	if mixed.CanAssign(viewer, "") {
		t.Error("assigned on * an action held only on p1")
	}
	var admin Grants
	admin.Add("", All)
	if !admin.CanAssign([]Action{All}, "") {
		t.Error("admin cannot assign admin")
	}
}

func TestCanAuthor(t *testing.T) {
	var g Grants
	g.Add("", PolicyManage, ContributionRead)
	g.Add("p1", SignalCreate)
	if !g.CanAuthor([]Action{ContributionRead}) {
		t.Error("cannot author an action held on *")
	}
	if g.CanAuthor([]Action{SignalCreate}) {
		t.Error("authored an action held only on a project")
	}
	var noManage Grants
	noManage.Add("", ContributionRead)
	if noManage.CanAuthor([]Action{ContributionRead}) {
		t.Error("authored without policy:Manage")
	}
}

func TestList(t *testing.T) {
	var g Grants
	g.Add("", SignalCreate, ContributionRead)
	g.Add("p1", MemberManage)
	all, projects := g.List()
	if !slices.Equal(all, []Action{ContributionRead, SignalCreate}) {
		t.Fatal(all)
	}
	if !slices.Equal(projects["p1"], []Action{MemberManage}) || len(projects) != 1 {
		t.Fatal(projects)
	}
	var admin Grants
	admin.Add("", All)
	if all, _ := admin.List(); !slices.Equal(all, []Action{All}) {
		t.Fatal(all)
	}
	var none Grants
	if all, projects := none.List(); all == nil || projects == nil {
		t.Fatal("List of zero Grants must be empty, not nil")
	}
}

func TestValidateCustom(t *testing.T) {
	got, err := ValidateCustom([]Action{SignalCreate, ContributionRead, SignalCreate})
	if err != nil || !slices.Equal(got, []Action{ContributionRead, SignalCreate}) {
		t.Fatal(got, err)
	}
	for _, bad := range [][]Action{nil, {All}, {"contribution:Delete"}} {
		if _, err := ValidateCustom(bad); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("ValidateCustom(%v) err = %v", bad, err)
		}
	}
}

func TestNormalizeName(t *testing.T) {
	if n, err := NormalizeName("  Reviewer  "); err != nil || n != "Reviewer" {
		t.Fatal(n, err)
	}
	long := make([]rune, 65)
	for i := range long {
		long[i] = 'a'
	}
	for _, bad := range []string{"", "   ", "a\nb", string(long)} {
		if _, err := NormalizeName(bad); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("NormalizeName(%q) err = %v", bad, err)
		}
	}
}

func TestCovers(t *testing.T) {
	var admin, mgr, viewer Grants
	admin.Add("", All)
	mgr.Add("", ActorManage, ContributionRead)
	mgr.Add("p1", SignalCreate)
	viewer.Add("p1", ContributionRead)
	if !admin.Covers(mgr) || !mgr.Covers(viewer) || !mgr.Covers(Grants{}) {
		t.Error("should cover")
	}
	if mgr.Covers(admin) {
		t.Error("non-admin covers admin")
	}
	var other Grants
	other.Add("p2", SignalCreate)
	if mgr.Covers(other) {
		t.Error("covers an action on a project it lacks")
	}
	var star Grants
	star.Add("", SignalCreate)
	if mgr.Covers(star) {
		t.Error("project grant covers the same action on *")
	}
}
