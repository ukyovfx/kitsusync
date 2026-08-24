package setup

import "testing"

func TestDiscordPermissionsFromRolesCombinesMemberRoles(t *testing.T) {
	permissions, err := discordPermissionsFromRoles("guild", []discordRolePermission{
		{ID: "guild", Permissions: "0"},
		{ID: "manager", Permissions: "536870928"},
	}, []string{"manager"})
	if err != nil {
		t.Fatal(err)
	}
	if !permissions.ManageChannels || !permissions.ManageWebhooks {
		t.Fatalf("expected both management permissions, got %+v", permissions)
	}
}

func TestDiscordPermissionsFromRolesAdministratorSatisfiesRequirements(t *testing.T) {
	permissions, err := discordPermissionsFromRoles("guild", []discordRolePermission{{ID: "guild", Permissions: "8"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !permissions.ManageChannels || !permissions.ManageWebhooks {
		t.Fatalf("administrator should satisfy management permissions, got %+v", permissions)
	}
}

func TestDiscordPermissionsFromRolesRejectsUnassignedRole(t *testing.T) {
	permissions, err := discordPermissionsFromRoles("guild", []discordRolePermission{
		{ID: "guild", Permissions: "0"},
		{ID: "manager", Permissions: "536870928"},
	}, []string{"other"})
	if err != nil {
		t.Fatal(err)
	}
	if permissions.ManageChannels || permissions.ManageWebhooks {
		t.Fatalf("unassigned role must not grant permissions, got %+v", permissions)
	}
}

func TestDiscordPermissionsFromRolesRejectsMalformedPermissionBits(t *testing.T) {
	if _, err := discordPermissionsFromRoles("guild", []discordRolePermission{{ID: "guild", Permissions: "not-a-number"}}, nil); err == nil {
		t.Fatal("expected malformed permission bits to fail closed")
	}
}
