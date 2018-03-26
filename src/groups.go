package main

import (
	"errors"
	"github.com/Syleron/PulseHA/src/netUtils"
	"github.com/Syleron/PulseHA/src/utils"
	log "github.com/Sirupsen/logrus"
	"strconv"
)

/**
Generate a new group in memory.
 */
func GroupNew(groupName string) (string, error) {
	gconf.Lock()
	defer gconf.Unlock()
	var name string
	if groupName != "" {
		name = groupName
	} else {
		name = GenGroupName()
	}
	gconf.Groups[name] = []string{}
	return name, nil
}

/**
Remove group from memory
 */
func GroupDelete(groupName string) error {
	gconf.Lock()
	defer gconf.Unlock()
	if GroupExist(groupName) {
		if !NodeAssignedToInterface(groupName) {
			delete(gconf.Groups, groupName)
			return nil
		}
		return errors.New("group has network interface assignments. Please remove them and try again")
	} else {
		return errors.New("unable to delete group that doesn't exist")
	}
}

/**
Clear out all local Groups
 */
func GroupClearLocal() {
	gconf.Lock()
	defer gconf.Unlock()
	gconf.Groups = map[string][]string{}
}

/**
Add floating IP to group
 */
func GroupIpAdd(groupName string, ips []string) error {
	gconf.Lock()
	defer gconf.Unlock()
	_, activeMember := pulse.Server.Memberlist.getActiveMember()
	if activeMember == nil {
		return errors.New("unable to add IP(s) to group as there no active node in the cluster")
	}
	if !GroupExist(groupName) {
		return errors.New("group does not exist")
	}
	for _, ip := range ips {
		if err := utils.ValidIPAddress(ip); err == nil {
			// Check to see if the IP exists in any of the groups
			if !GroupIpExistAll(ip) {
				gconf.Groups[groupName] = append(gconf.Groups[groupName], ip)
			} else {
				return errors.New(ip + " already exists in group " + groupName + ".. skipping.")
			}
		} else {
			return err
		}
	}
	return nil
}

/**
Remove floating IP from group
 */
func GroupIpRemove(groupName string, ips []string) error {
	gconf.Lock()
	defer gconf.Unlock()
	if !GroupExist(groupName) {
		return errors.New("group does not exist")
	}
	for _, ip := range ips {
		if len(gconf.Groups[groupName]) > 0 {
			if exists, i := GroupIPExist(groupName, ip); exists {
				gconf.Groups[groupName] = append(gconf.Groups[groupName][:i], gconf.Groups[groupName][i+1:]...)
			} else {
				log.Warning(ip + " does not exist in group " + groupName + ".. skipping.")
			}
		}
	}
	return nil
}

/**
Assign a group to a node's interface
 */
func GroupAssign(groupName, node, iface string) error {
	gconf.Lock()
	defer gconf.Unlock()
	if !gconf.ClusterCheck() {
		return errors.New("action can only be completed in a configured cluster")
	}
	if !GroupExist(groupName) {
		return errors.New("IP group does not exist")
	}
	if netUtils.InterfaceExist(iface) {
		if exists, _ := NodeInterfaceGroupExists(node, iface, groupName); !exists {
			// Add the group
			gconf.Nodes[node].IPGroups[iface] = append(gconf.Nodes[node].IPGroups[iface], groupName)
			// make the group active
			makeGroupActive(iface, groupName)
		} else {
			log.Warning(groupName + " already exists in node " + node + ".. skipping.")
		}
		return nil
	}
	return errors.New("interface does not exist")
}

/**
Unassign a group from a node's interface
 */
func GroupUnassign(groupName, node, iface string) error {
	gconf.Lock()
	defer gconf.Unlock()
	if !GroupExist(groupName) {
		return errors.New("IP group does not exist")
	}
	if netUtils.InterfaceExist(iface) {
		if exists, i := NodeInterfaceGroupExists(node, iface, groupName); exists {
			// make the group passive before removing it
			makeGroupPassive(iface, groupName)
			// Remove it
			gconf.Nodes[node].IPGroups[iface] = append(gconf.Nodes[node].IPGroups[iface][:i], gconf.Nodes[node].IPGroups[iface][i+1:]...)
		} else {
			log.Warning(groupName + " does not exist in node " + node + ".. skipping.")
		}
		return nil
	}
	return errors.New("interface does not exist")
}

/**
Generates an available IP floating group name.
 */
func GenGroupName() string {
	config := gconf.GetConfig()
	totalGroups := len(config.Groups)
	for i := 1; i <= totalGroups; i++ {
		newName := "group" + strconv.Itoa(i)
		if _, ok := config.Groups[newName]; !ok {
			return newName
		}
	}
	return "group" + strconv.Itoa(totalGroups+1)
}

/**
Checks to see if a floating IP group already exists
 */
func GroupExist(name string) bool {
	config := gconf.GetConfig()
	if _, ok := config.Groups[name]; ok {
		return true
	}
	return false
}

/**
Checks to see if a floating IP already exists inside of a floating ip group
 */
func GroupIPExist(name string, ip string) (bool, int) {
	config := gconf.GetConfig()
	for index, cip := range config.Groups[name] {
		if ip == cip {
			return true, index
		}
	}
	return false, -1
}

/**
Checks to see if a floating IP already exists in any of the floating IP groups
 */
func GroupIpExistAll(ip string) (bool) {
	config := gconf.GetConfig()
	for _, cip := range config.Groups {
		for _, dip := range cip {
			if ip == dip {
				return true
			}
		}
	}
	return false
}

/**
function to get the nodes and interfaces that relate to the specified node
 */
func getGroupNodes(group string) ([]string, []string) {
	var hosts []string
	var interfaces []string
	var found = false
	config := gconf.GetConfig()
	for name, node := range config.Nodes {
		for iface, groupNameSlice := range node.IPGroups {
			for _, groupName := range groupNameSlice {
				if group == groupName {
					hosts = append(hosts, name)
					interfaces = append(interfaces, iface)
					found = true
				}
			}
		}
	}
	if found {
		return hosts, interfaces
	}
	return nil, nil
}

/**
Make a group of IPs active
 */
func makeGroupActive(iface string, groupName string) {
	//log.Infof("Bringing up IPs on Interface: %s, Group: %s", iface, groupName)
	// gconf.Reload()
	configCopy := gconf.GetConfig()
	bringUpIPs(iface, configCopy.Groups[groupName])
	//garp?
}

func makeGroupPassive(iface string, groupName string) {
	//log.Infof("Bringing down IPs on Interface: %s, Group: %s", iface, groupName)
	// gconf.Reload()
	configCopy := gconf.GetConfig()
	bringDownIPs(iface, configCopy.Groups[groupName])
	//garp?
}
