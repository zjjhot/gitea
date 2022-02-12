// Copyright 2014 The Gogs Authors. All rights reserved.
// Copyright 2020 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package ldap

import (
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"strings"

	"code.gitea.io/gitea/modules/json"
	"code.gitea.io/gitea/modules/log"
	"code.gitea.io/gitea/modules/util"

	"github.com/go-ldap/ldap/v3"
)

// SearchResult : user data
type SearchResult struct {
	Username       string   // Username
	Name           string   // Name
	Surname        string   // Surname
	Mail           string   // E-mail address
	SSHPublicKey   []string // SSH Public Key
	IsAdmin        bool     // if user is administrator
	IsRestricted   bool     // if user is restricted
	LowerName      string   // LowerName
	Avatar         []byte
	LdapTeamAdd    map[string][]string // organizations teams to add
	LdapTeamRemove map[string][]string // organizations teams to remove
}

func (ls *Source) sanitizedUserQuery(username string) (string, bool) {
	// See http://tools.ietf.org/search/rfc4515
	badCharacters := "\x00()*\\"
	if strings.ContainsAny(username, badCharacters) {
		log.Debug("'%s' contains invalid query characters. Aborting.", username)
		return "", false
	}

	return fmt.Sprintf(ls.Filter, username), true
}

func (ls *Source) sanitizedUserDN(username string) (string, bool) {
	// See http://tools.ietf.org/search/rfc4514: "special characters"
	badCharacters := "\x00()*\\,='\"#+;<>"
	if strings.ContainsAny(username, badCharacters) {
		log.Debug("'%s' contains invalid DN characters. Aborting.", username)
		return "", false
	}

	return fmt.Sprintf(ls.UserDN, username), true
}

func (ls *Source) sanitizedGroupFilter(group string) (string, bool) {
	// See http://tools.ietf.org/search/rfc4515
	badCharacters := "\x00*\\"
	if strings.ContainsAny(group, badCharacters) {
		log.Trace("Group filter invalid query characters: %s", group)
		return "", false
	}

	return group, true
}

func (ls *Source) sanitizedGroupDN(groupDn string) (string, bool) {
	// See http://tools.ietf.org/search/rfc4514: "special characters"
	badCharacters := "\x00()*\\'\"#+;<>"
	if strings.ContainsAny(groupDn, badCharacters) || strings.HasPrefix(groupDn, " ") || strings.HasSuffix(groupDn, " ") {
		log.Trace("Group DN contains invalid query characters: %s", groupDn)
		return "", false
	}

	return groupDn, true
}

func (ls *Source) findUserDN(l *ldap.Conn, name string) (string, bool) {
	log.Trace("Search for LDAP user: %s", name)

	// A search for the user.
	userFilter, ok := ls.sanitizedUserQuery(name)
	if !ok {
		return "", false
	}

	log.Trace("Searching for DN using filter %s and base %s", userFilter, ls.UserBase)
	search := ldap.NewSearchRequest(
		ls.UserBase, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0,
		false, userFilter, []string{}, nil)

	// Ensure we found a user
	sr, err := l.Search(search)
	if err != nil || len(sr.Entries) < 1 {
		log.Debug("Failed search using filter[%s]: %v", userFilter, err)
		return "", false
	} else if len(sr.Entries) > 1 {
		log.Debug("Filter '%s' returned more than one user.", userFilter)
		return "", false
	}

	userDN := sr.Entries[0].DN
	if userDN == "" {
		log.Error("LDAP search was successful, but found no DN!")
		return "", false
	}

	return userDN, true
}

func dial(source *Source) (*ldap.Conn, error) {
	log.Trace("Dialing LDAP with security protocol (%v) without verifying: %v", source.SecurityProtocol, source.SkipVerify)

	tlsConfig := &tls.Config{
		ServerName:         source.Host,
		InsecureSkipVerify: source.SkipVerify,
	}

	if source.SecurityProtocol == SecurityProtocolLDAPS {
		return ldap.DialTLS("tcp", net.JoinHostPort(source.Host, strconv.Itoa(source.Port)), tlsConfig)
	}

	conn, err := ldap.Dial("tcp", net.JoinHostPort(source.Host, strconv.Itoa(source.Port)))
	if err != nil {
		return nil, fmt.Errorf("error during Dial: %v", err)
	}

	if source.SecurityProtocol == SecurityProtocolStartTLS {
		if err = conn.StartTLS(tlsConfig); err != nil {
			conn.Close()
			return nil, fmt.Errorf("error during StartTLS: %v", err)
		}
	}

	return conn, nil
}

func bindUser(l *ldap.Conn, userDN, passwd string) error {
	log.Trace("Binding with userDN: %s", userDN)
	err := l.Bind(userDN, passwd)
	if err != nil {
		log.Debug("LDAP auth. failed for %s, reason: %v", userDN, err)
		return err
	}
	log.Trace("Bound successfully with userDN: %s", userDN)
	return err
}

func checkAdmin(l *ldap.Conn, ls *Source, userDN string) bool {
	if len(ls.AdminFilter) == 0 {
		return false
	}
	log.Trace("Checking admin with filter %s and base %s", ls.AdminFilter, userDN)
	search := ldap.NewSearchRequest(
		userDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false, ls.AdminFilter,
		[]string{ls.AttributeName},
		nil)

	sr, err := l.Search(search)

	if err != nil {
		log.Error("LDAP Admin Search with filter %s for %s failed unexpectedly! (%v)", ls.AdminFilter, userDN, err)
	} else if len(sr.Entries) < 1 {
		log.Trace("LDAP Admin Search found no matching entries.")
	} else {
		return true
	}
	return false
}

func checkRestricted(l *ldap.Conn, ls *Source, userDN string) bool {
	if len(ls.RestrictedFilter) == 0 {
		return false
	}
	if ls.RestrictedFilter == "*" {
		return true
	}
	log.Trace("Checking restricted with filter %s and base %s", ls.RestrictedFilter, userDN)
	search := ldap.NewSearchRequest(
		userDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false, ls.RestrictedFilter,
		[]string{ls.AttributeName},
		nil)

	sr, err := l.Search(search)

	if err != nil {
		log.Error("LDAP Restrictred Search with filter %s for %s failed unexpectedly! (%v)", ls.RestrictedFilter, userDN, err)
	} else if len(sr.Entries) < 1 {
		log.Trace("LDAP Restricted Search found no matching entries.")
	} else {
		return true
	}
	return false
}

// List all group memberships of a user
func (ls *Source) listLdapGroupMemberships(l *ldap.Conn, uid string) []string {
	var ldapGroups []string
	groupFilter := fmt.Sprintf("(%s=%s)", ls.GroupMemberUID, uid)
	result, err := l.Search(ldap.NewSearchRequest(
		ls.GroupDN,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		0,
		0,
		false,
		groupFilter,
		[]string{},
		nil,
	))
	if err != nil {
		log.Error("Failed group search using filter[%s]: %v", groupFilter, err)
		return ldapGroups
	}

	for _, entry := range result.Entries {
		if entry.DN == "" {
			log.Error("LDAP search was successful, but found no DN!")
			continue
		}
		ldapGroups = append(ldapGroups, entry.DN)
	}

	return ldapGroups
}

// parse LDAP groups and return map of ldap groups to organizations teams
func (ls *Source) mapLdapGroupsToTeams() map[string]map[string][]string {
	ldapGroupsToTeams := make(map[string]map[string][]string)
	err := json.Unmarshal([]byte(ls.GroupTeamMap), &ldapGroupsToTeams)
	if err != nil {
		log.Error("Failed to unmarshall LDAP teams map: %v", err)
		return ldapGroupsToTeams
	}
	return ldapGroupsToTeams
}

// getMappedMemberships : returns the organizations and teams to modify the users membership
func (ls *Source) getMappedMemberships(l *ldap.Conn, uid string) (map[string][]string, map[string][]string) {
	// get all LDAP group memberships for user
	usersLdapGroups := ls.listLdapGroupMemberships(l, uid)
	// unmarshall LDAP group team map from configs
	ldapGroupsToTeams := ls.mapLdapGroupsToTeams()
	membershipsToAdd := map[string][]string{}
	membershipsToRemove := map[string][]string{}
	for group, memberships := range ldapGroupsToTeams {
		isUserInGroup := util.IsStringInSlice(group, usersLdapGroups)
		if isUserInGroup {
			for org, teams := range memberships {
				membershipsToAdd[org] = teams
			}
		} else if !isUserInGroup {
			for org, teams := range memberships {
				membershipsToRemove[org] = teams
			}
		}
	}
	return membershipsToAdd, membershipsToRemove
}

// SearchEntry : search an LDAP source if an entry (name, passwd) is valid and in the specific filter
func (ls *Source) SearchEntry(name, passwd string, directBind bool) *SearchResult {
	// See https://tools.ietf.org/search/rfc4513#section-5.1.2
	if len(passwd) == 0 {
		log.Debug("Auth. failed for %s, password cannot be empty", name)
		return nil
	}
	l, err := dial(ls)
	if err != nil {
		log.Error("LDAP Connect error, %s:%v", ls.Host, err)
		ls.Enabled = false
		return nil
	}
	defer l.Close()

	var userDN string
	if directBind {
		log.Trace("LDAP will bind directly via UserDN template: %s", ls.UserDN)

		var ok bool
		userDN, ok = ls.sanitizedUserDN(name)

		if !ok {
			return nil
		}

		err = bindUser(l, userDN, passwd)
		if err != nil {
			return nil
		}

		if ls.UserBase != "" {
			// not everyone has a CN compatible with input name so we need to find
			// the real userDN in that case

			userDN, ok = ls.findUserDN(l, name)
			if !ok {
				return nil
			}
		}
	} else {
		log.Trace("LDAP will use BindDN.")

		var found bool

		if ls.BindDN != "" && ls.BindPassword != "" {
			err := l.Bind(ls.BindDN, ls.BindPassword)
			if err != nil {
				log.Debug("Failed to bind as BindDN[%s]: %v", ls.BindDN, err)
				return nil
			}
			log.Trace("Bound as BindDN %s", ls.BindDN)
		} else {
			log.Trace("Proceeding with anonymous LDAP search.")
		}

		userDN, found = ls.findUserDN(l, name)
		if !found {
			return nil
		}
	}

	if !ls.AttributesInBind {
		// binds user (checking password) before looking-up attributes in user context
		err = bindUser(l, userDN, passwd)
		if err != nil {
			return nil
		}
	}

	userFilter, ok := ls.sanitizedUserQuery(name)
	if !ok {
		return nil
	}

	isAttributeSSHPublicKeySet := len(strings.TrimSpace(ls.AttributeSSHPublicKey)) > 0
	isAtributeAvatarSet := len(strings.TrimSpace(ls.AttributeAvatar)) > 0

	attribs := []string{ls.AttributeUsername, ls.AttributeName, ls.AttributeSurname, ls.AttributeMail}
	if len(strings.TrimSpace(ls.UserUID)) > 0 {
		attribs = append(attribs, ls.UserUID)
	}
	if isAttributeSSHPublicKeySet {
		attribs = append(attribs, ls.AttributeSSHPublicKey)
	}
	if isAtributeAvatarSet {
		attribs = append(attribs, ls.AttributeAvatar)
	}

	log.Trace("Fetching attributes '%v', '%v', '%v', '%v', '%v', '%v', '%v' with filter '%s' and base '%s'", ls.AttributeUsername, ls.AttributeName, ls.AttributeSurname, ls.AttributeMail, ls.AttributeSSHPublicKey, ls.AttributeAvatar, ls.UserUID, userFilter, userDN)
	search := ldap.NewSearchRequest(
		userDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false, userFilter,
		attribs, nil)

	sr, err := l.Search(search)
	if err != nil {
		log.Error("LDAP Search failed unexpectedly! (%v)", err)
		return nil
	} else if len(sr.Entries) < 1 {
		if directBind {
			log.Trace("User filter inhibited user login.")
		} else {
			log.Trace("LDAP Search found no matching entries.")
		}

		return nil
	}

	var sshPublicKey []string
	var Avatar []byte

	username := sr.Entries[0].GetAttributeValue(ls.AttributeUsername)
	firstname := sr.Entries[0].GetAttributeValue(ls.AttributeName)
	surname := sr.Entries[0].GetAttributeValue(ls.AttributeSurname)
	mail := sr.Entries[0].GetAttributeValue(ls.AttributeMail)
	uid := sr.Entries[0].GetAttributeValue(ls.UserUID)
	if ls.UserUID == "dn" || ls.UserUID == "DN" {
		uid = sr.Entries[0].DN
	}

	// Check group membership
	if ls.GroupsEnabled && ls.GroupFilter != "" {
		groupFilter, ok := ls.sanitizedGroupFilter(ls.GroupFilter)
		if !ok {
			return nil
		}
		groupDN, ok := ls.sanitizedGroupDN(ls.GroupDN)
		if !ok {
			return nil
		}

		log.Trace("Fetching groups '%v' with filter '%s' and base '%s'", ls.GroupMemberUID, groupFilter, groupDN)
		groupSearch := ldap.NewSearchRequest(
			groupDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false, groupFilter,
			[]string{ls.GroupMemberUID},
			nil)

		srg, err := l.Search(groupSearch)
		if err != nil {
			log.Error("LDAP group search failed: %v", err)
			return nil
		} else if len(srg.Entries) < 1 {
			log.Error("LDAP group search failed: 0 entries")
			return nil
		}

		isMember := false
	Entries:
		for _, group := range srg.Entries {
			for _, member := range group.GetAttributeValues(ls.GroupMemberUID) {
				if (ls.UserUID == "dn" && member == sr.Entries[0].DN) || member == uid {
					isMember = true
					break Entries
				}
			}
		}

		if !isMember {
			log.Error("LDAP group membership test failed")
			return nil
		}
	}

	if isAttributeSSHPublicKeySet {
		sshPublicKey = sr.Entries[0].GetAttributeValues(ls.AttributeSSHPublicKey)
	}
	isAdmin := checkAdmin(l, ls, userDN)
	var isRestricted bool
	if !isAdmin {
		isRestricted = checkRestricted(l, ls, userDN)
	}

	if !directBind && ls.AttributesInBind {
		// binds user (checking password) after looking-up attributes in BindDN context
		err = bindUser(l, userDN, passwd)
		if err != nil {
			return nil
		}
	}

	if isAtributeAvatarSet {
		Avatar = sr.Entries[0].GetRawAttributeValue(ls.AttributeAvatar)
	}

	teamsToAdd := make(map[string][]string)
	teamsToRemove := make(map[string][]string)
	if ls.GroupsEnabled && (ls.GroupTeamMap != "" || ls.GroupTeamMapRemoval) {
		teamsToAdd, teamsToRemove = ls.getMappedMemberships(l, uid)
	}

	return &SearchResult{
		LowerName:      strings.ToLower(username),
		Username:       username,
		Name:           firstname,
		Surname:        surname,
		Mail:           mail,
		SSHPublicKey:   sshPublicKey,
		IsAdmin:        isAdmin,
		IsRestricted:   isRestricted,
		Avatar:         Avatar,
		LdapTeamAdd:    teamsToAdd,
		LdapTeamRemove: teamsToRemove,
	}
}

// UsePagedSearch returns if need to use paged search
func (ls *Source) UsePagedSearch() bool {
	return ls.SearchPageSize > 0
}

// SearchEntries : search an LDAP source for all users matching userFilter
func (ls *Source) SearchEntries() ([]*SearchResult, error) {
	l, err := dial(ls)
	if err != nil {
		log.Error("LDAP Connect error, %s:%v", ls.Host, err)
		ls.Enabled = false
		return nil, err
	}
	defer l.Close()

	if ls.BindDN != "" && ls.BindPassword != "" {
		err := l.Bind(ls.BindDN, ls.BindPassword)
		if err != nil {
			log.Debug("Failed to bind as BindDN[%s]: %v", ls.BindDN, err)
			return nil, err
		}
		log.Trace("Bound as BindDN %s", ls.BindDN)
	} else {
		log.Trace("Proceeding with anonymous LDAP search.")
	}

	userFilter := fmt.Sprintf(ls.Filter, "*")

	isAttributeSSHPublicKeySet := len(strings.TrimSpace(ls.AttributeSSHPublicKey)) > 0
	isAtributeAvatarSet := len(strings.TrimSpace(ls.AttributeAvatar)) > 0

	attribs := []string{ls.AttributeUsername, ls.AttributeName, ls.AttributeSurname, ls.AttributeMail, ls.UserUID}
	if isAttributeSSHPublicKeySet {
		attribs = append(attribs, ls.AttributeSSHPublicKey)
	}
	if isAtributeAvatarSet {
		attribs = append(attribs, ls.AttributeAvatar)
	}

	log.Trace("Fetching attributes '%v', '%v', '%v', '%v', '%v', '%v' with filter %s and base %s", ls.AttributeUsername, ls.AttributeName, ls.AttributeSurname, ls.AttributeMail, ls.AttributeSSHPublicKey, ls.AttributeAvatar, userFilter, ls.UserBase)
	search := ldap.NewSearchRequest(
		ls.UserBase, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false, userFilter,
		attribs, nil)

	var sr *ldap.SearchResult
	if ls.UsePagedSearch() {
		sr, err = l.SearchWithPaging(search, ls.SearchPageSize)
	} else {
		sr, err = l.Search(search)
	}
	if err != nil {
		log.Error("LDAP Search failed unexpectedly! (%v)", err)
		return nil, err
	}

	result := make([]*SearchResult, len(sr.Entries))

	for i, v := range sr.Entries {
		teamsToAdd := make(map[string][]string)
		teamsToRemove := make(map[string][]string)
		if ls.GroupsEnabled && (ls.GroupTeamMap != "" || ls.GroupTeamMapRemoval) {
			userAttributeListedInGroup := v.GetAttributeValue(ls.UserUID)
			if ls.UserUID == "dn" || ls.UserUID == "DN" {
				userAttributeListedInGroup = v.DN
			}
			teamsToAdd, teamsToRemove = ls.getMappedMemberships(l, userAttributeListedInGroup)
		}
		result[i] = &SearchResult{
			Username:       v.GetAttributeValue(ls.AttributeUsername),
			Name:           v.GetAttributeValue(ls.AttributeName),
			Surname:        v.GetAttributeValue(ls.AttributeSurname),
			Mail:           v.GetAttributeValue(ls.AttributeMail),
			IsAdmin:        checkAdmin(l, ls, v.DN),
			LdapTeamAdd:    teamsToAdd,
			LdapTeamRemove: teamsToRemove,
		}
		if !result[i].IsAdmin {
			result[i].IsRestricted = checkRestricted(l, ls, v.DN)
		}
		if isAttributeSSHPublicKeySet {
			result[i].SSHPublicKey = v.GetAttributeValues(ls.AttributeSSHPublicKey)
		}
		if isAtributeAvatarSet {
			result[i].Avatar = v.GetRawAttributeValue(ls.AttributeAvatar)
		}
		result[i].LowerName = strings.ToLower(result[i].Username)
	}

	return result, nil
}
