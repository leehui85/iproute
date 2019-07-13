package iproute

import (
	"bytes"
	"fmt"
	//netns "github.com/hariguchi/go_netns"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"testing"
)

func testVrfAdd(t *testing.T) Vrf {
	vrf := Vrf{
		Name: "",
		Tid:  1,
	}
	for {
		vrf.Name = fmt.Sprintf("myVRF%02d", vrf.Tid)
		if _, err := VrfGetByName(vrf.Name); err == nil {
			t.Logf("VRF %s already exists.", vrf.Name)
			vrf.Tid++
		} else {
			break
		}
	}
	t.Logf("Adding VRF %s (tid %d)... ", vrf.Name, vrf.Tid)
	if err := VrfAdd(vrf.Name, vrf.Tid); err == nil {
		if v, err := VrfGetByName(vrf.Name); err == nil {
			if v.Name == vrf.Name && v.Tid == vrf.Tid {
				vrf.Index = v.Index
				t.Logf("confirmed.")
			} else {
				t.Fatalf("Error: VRF %s, tid %d", v.Name, v.Tid)
			}
		} else {
			t.Fatalf("Error: no such VRF exists.")
		}
	} else {
		t.Fatalf("Error: failed to add.")
	}
	return vrf
}

func testVrfDelete(t *testing.T, vrf *Vrf) {
	t.Logf("Deleting VRF %s... ", vrf.Name)

	if _, err := VrfGetByName(vrf.Name); err == nil {
		if err := VrfDelete(vrf.Name); err == nil {
			t.Logf("deleted... ")
			if _, err := VrfGetByName(vrf.Name); err != nil {
				t.Logf("confirmed.")
			} else {
				t.Fatalf("Error: VRF %s still exists.", vrf.Name)
			}
		} else {
			t.Fatalf("Error: failed to delete VRF %s", vrf.Name)
		}
	} else {
		t.Fatalf("Error: no such VRF: %s", vrf.Name)
	}
}

func testVethAdd(t *testing.T) Veth {
	var (
		veth Veth
		i    int
	)

	for i = 0; true; i++ {
		veth.Name = fmt.Sprintf("foo%02d-bar%02d", i, i)
		veth.Peer = fmt.Sprintf("bar%02d-foo%02d", i, i)
		if ve, err := VethGetByName(veth.Name); err == nil {
			if veth.Name != ve.Name {
				t.Fatalf("%s, %s", veth.Name, ve.Name)
			}
			if veth.Peer != ve.Peer {
				t.Fatalf("%s, %s", veth.Peer, ve.Peer)
			}
			t.Logf("veth pair %s - %s already exists.", ve.Name, ve.Peer)
		} else if ve.IsNotFound(err) {
			t.Logf("veth pair %s - %s doesn't exist.", veth.Name, veth.Peer)
			break
		} else {
			t.Fatal(err)
		}
	}

	t.Logf("Adding veth pair: %s - %s...", veth.Name, veth.Peer)
	if ve, err := VethAdd(veth.Name, veth.Peer, false); err == nil {
		if ve.Name != veth.Name {
			t.Fatalf("%s: %s", ve.Name, veth.Name)
		}
		if ve.Peer != veth.Peer {
			t.Fatalf("%s: %s", ve.Peer, veth.Peer)
		}
		if r, err := VethGetByName(veth.Peer); err == nil {
			if r.Name == veth.Peer && r.Peer == veth.Name {
				if r.TxQlen != DefaultTxQlen {
					t.Logf("  TxQlen: %d - %d\n", DefaultTxQlen, r.TxQlen)
				}
				if r.MTU != DefaultMTU {
					t.Logf("  MTU: %d - %d\n", DefaultMTU, r.MTU)
				}
				t.Logf("confirmed.")
			} else {
				t.Fatalf("Error: in: %s - %s, out: %s - %s",
					veth.Name, veth.Peer, r.Peer, r.Name)
			}
		} else {
			t.Fatalf("Error: VethGetByName(%s): %v", veth.Peer, err)
		}
	} else {
		t.Fatalf("Error: VethAdd(%s, %s): %v", veth.Name, veth.Peer, err)
	}
	return veth
}

func testVethDelete(t *testing.T, veth *Veth) {
	t.Logf("Deleting veth pair %s - %s... ", veth.Name, veth.Peer)

	if _, err := VethGetByName(veth.Peer); err == nil {
		if err := VethDelete(veth.Peer); err == nil {
			t.Logf("confirmed.")
		} else {
			t.Fatalf("Error: VethDelete(%s): %v", veth.Peer, err)
		}
	} else {
		t.Fatalf("Error: VethGetByName(%s): no such veth exists.", veth.Peer)
	}
}

func testVlanDelete(t *testing.T, vlan string) {
	t.Logf("Deleting VLAN %s... ", vlan)
	if err := VlanDelete(vlan); err == nil {
		t.Logf("confirmed.")
	} else {
		t.Errorf("Error: VlanDelete(%s): %v", vlan, err)
	}
}

func testIpAddrAdd(t *testing.T, veth *Veth, vlanId uint16, ifa []string) {
	names := []string{
		fmt.Sprintf("%s.%d", veth.Name, vlanId),
		fmt.Sprintf("%s.%d", veth.Peer, vlanId),
	}
	for i, name := range names {
		t.Logf("Adding %s to %s", ifa[i], name)
		if addr, net, err := net.ParseCIDR(ifa[i]); err == nil {
			net.IP = addr
			if err := IpAddrAdd(name, net, true); err == nil {
				if ok, err := IsIfPrefix(name, net); err == nil {
					if !ok {
						t.Errorf("Error: failed to %s to %s", ifa[i], name)
					}
				} else {
					t.Errorf("Error: IsIfPrefix(%s, %s): %v", name, ifa[i], err)
				}
			} else {
				t.Errorf("Error: IpAddrAdd(%s, %s): %v", name, ifa[i], err)
			}
		} else {
			t.Errorf("Error: ParseCIDR(%s): %v", ifa[i], err)
		}
	}
}

func testRename(t *testing.T, vrf *Vrf, name string) {
	errMsg := fmt.Sprintf("Error: IfRename(%s, %s): ", vrf.Name, name)
	t.Logf("Renaming %s to %s...", vrf.Name, name)
	if err := IfRename(vrf.Name, name); err == nil {
		if v, err := VrfGetByName(name); err == nil {
			if v.Name == name && v.Tid == vrf.Tid {
				t.Logf("confirmed.")
				vrf.Name = name
			} else {
				t.Errorf(errMsg+
					"name: %s (should be %s), Tid: %d (should be %d), %v",
					v.Name, name, v.Tid, vrf.Tid, err)
			}
		} else {
			t.Errorf(errMsg+"VrfGetByName(%s): %v", name, err)
		}
	} else {
		t.Errorf(errMsg+"%v", err)
	}
}

func vrfVethVlanAdd(t *testing.T, vlanId uint16) (Vrf, Veth) {
	var vlanIfs []string

	// 1. Create a vrf
	// 2. Make sure it is down
	// 3. Bring it up
	// 4. Make sure it is up
	//
	vrf := testVrfAdd(t)
	if r, err := IfIsUpByName(vrf.Name); err == nil {
		if r == true {
			t.Errorf("Error: VRF %s should be DOWN", vrf.Name)
		}
		if err := IfUpByName(vrf.Name); err != nil {
			t.Errorf("Error: Failed to bring up %s", vrf.Name)
		}
	} else {
		t.Errorf("Error: IfIsUpByName(%s): %v", vrf.Name, err)
	}

	// 1. Add veth pair
	// 2. Make sure both veth interfaces are down
	// 3. Bring them up
	//
	veth := testVethAdd(t)
	for _, name := range []string{veth.Name, veth.Peer} {
		if ok, err := IfIsUpByName(name); err == nil {
			if ok {
				t.Errorf("Error: %s should be down", name)
			}
			t.Logf("Bringing up %s...", name)
			if err := IfUpByName(name); err == nil {
				if ok, err := IfIsUpByName(name); err == nil {
					if ok {
						t.Logf("confirmed.")
					} else {
						t.Errorf("Error: %s should be up.", name)
					}
				}
			} else {
				t.Errorf("Error: IfUpByName(%s): %v", name, err)
			}
		} else {
			t.Errorf("Error: IfIsUpByName(%s): %v", name, err)
		}
	}

	// 1. Add VLAN interfaces to veth interface pair
	// 2. Make sure they are down
	// 3. Bring them up
	//
	if s, err := VlanAdd(veth.Name, vlanId); err == nil {
		exp := fmt.Sprintf("%s.%d", veth.Name, vlanId)
		if s == exp {
			vlanIfs = append(vlanIfs, s)
			if ok, err := IfIsUpByName(s); err == nil {
				if ok {
					t.Errorf("Error: %s should be down", s)
				}
			} else {
				t.Errorf("Error: IfIsUpByName(%s): %v", s, err)
			}
		} else {
			t.Errorf("Error: %s: should be %s", s, exp)
		}
	} else {
		t.Errorf("Error: VlanAdd(%d, %s): %v", vlanId, veth.Name, err)
	}
	if s, err := VlanAdd(veth.Peer, vlanId); err == nil {
		exp := fmt.Sprintf("%s.%d", veth.Peer, vlanId)
		if s == exp {
			vlanIfs = append(vlanIfs, s)
			if ok, err := IfIsUpByName(s); err == nil {
				if ok {
					t.Errorf("Error: %s should be down", s)
				}
			} else {
				t.Errorf("Error: IfIsUpByName(%s): %v", s, err)
			}
		} else {
			t.Errorf("Error: %s: should be %s", s, exp)
		}
	} else {
		t.Errorf("Error: VlanAdd(%d, %s): %v", vlanId, veth.Peer, err)
	}

	for _, name := range vlanIfs {
		if err := VrfBindIntf(vrf.Name, name); err == nil {
			if err := IfUpByName(name); err == nil {
				if ok, err := IfIsUpByName(name); err == nil {
					if !ok {
						t.Errorf("Error: %s should be up", name)
					}
				} else {
					t.Errorf("Error: IfIsUpByName(%s): %v", name, err)
				}
			} else {
				t.Errorf("Error: IfUpByName(%s): %v", name, err)
			}
		} else {
			t.Errorf("Error: VrfBindIntf(%s, %s): %v", vrf.Name, name, err)
		}
	}
	return vrf, veth
}

func testPing(t *testing.T, vrf *Vrf, ifPrefix []string) {
	var (
		ifAddr  []string
		pingMsg = regexp.MustCompile(`0% packet loss`)
	)
	for _, prefix := range ifPrefix {
		if ipa, _, err := net.ParseCIDR(prefix); err == nil {
			ifAddr = append(ifAddr, ipa.String())
		} else {
			t.Errorf("Error: testPing(): ParseCIDR(%s): %v", prefix, err)
			return
		}
	}
	if fp, err := ioutil.TempFile("", "vrfping"); err == nil {
		defer os.Remove(fp.Name())

		cmd := fmt.Sprintf("#!/bin/sh\nVRF=%s ", vrf.Name)
		cmd += "LD_PRELOAD=/usr/bin/vrf_socket.so "
		cmd += fmt.Sprintf("ping -q -c 5 -I %s %s\n", ifAddr[0], ifAddr[1])
		if _, err := fp.WriteString(cmd); err == nil {
			name := fp.Name()
			if err := fp.Close(); err == nil {
				if err := os.Chmod(name, 0755); err == nil {
					t.Logf("Ping test:\n%s\n...", cmd)
					if out, err := exec.Command(name).Output(); err == nil {
						if pingMsg.MatchString(string(out)) {
							t.Logf("confirmed.")
						} else {
							t.Errorf("Error: testPing():\n%s\n", string(out))
						}
					} else {
						t.Errorf("testPing(): Command(%s): %v", name, err)
					}
				} else {
					t.Errorf("testPing(): Chmod(): %v", err)
				}
			} else {
				t.Errorf("testPing(): Close(): %v", err)
			}
		} else {
			t.Errorf("testPing(): WriteString(%s): %v", cmd, err)
		}
	} else {
		t.Errorf("testPing(): Tempfile(): %v", err)
	}
}

func testRoutes(t *testing.T, vrf *Vrf, dstName, nhName string) {
	errMsg := fmt.Sprintf("Error: testRoutes(): ")
	var rt *Route
	if _, dst, err := net.ParseCIDR(dstName); err == nil {
		nha := net.ParseIP(nhName)
		nh := IPs{nha}
		if r, err := NewRoute(dst, nh); err == nil {
			rt = &r
			t.Logf("Addring route: %s nh %s", dst, nhName)
			if err := VrfAddRouteByName(vrf.Name, rt); err != nil {
				t.Errorf(errMsg+
					"VrfAddRouteByName(%s, %v): %v", vrf.Name, rt, err)
				return
			}
		} else {
			t.Errorf(errMsg+"NewRoute(%v, %v): %v", dst, nh, err)
			return
		}
		//
		// verify rt is in the Kernel FIB
		//
		t.Logf("rt: %v", rt)
		if routes, err := VrfGetIPv4routesByName(vrf.Name); err == nil {
			t.Logf(" all routes:\n%v", routes)
			err = fmt.Errorf(errMsg+"%s nh %s: not exists", dst, nhName)
			for _, r := range routes {
				if IPNetEqual(r.Dst, rt.Dst) && r.Gw != nil && r.Gw.Equal(nha) {
					t.Logf("confirmed.")
					err = nil
					break
				}
			}
			if err != nil {
				t.Error(err)
				return
			}
		}
	} else {
		t.Errorf(errMsg+"ParseCIDR(): %v", err)
		return
	}
}

func testVrfOf(t *testing.T, vrf *Vrf, ifName string) {
	t.Logf("Testing VrfOf(%s)...", ifName)
	if v, err := VrfOf(ifName); err == nil {
		if v.Equal(vrf) {
			t.Logf("confirmed.")
		} else {
			t.Errorf("Error: VrfOf(): expected: %v, returned: %v", vrf, v)
		}
	} else {
		t.Errorf("Error: VrfOf(%s): %v", ifName, err)
	}
}

func testIPsortFail(t *testing.T, nh IPs) {
	defer func() {
		if r := recover(); r != nil {
			t.Logf("recovered: %v", r)
		}
	}()
	t.Logf("%v", nh)
	sort.Sort(nh)
	t.Logf("%v", nh)
}

func TestIPsortFailure(t *testing.T) {
	t.Logf("Testing IP address sorting failure...")
	var nh IPs
	nh = append(nh, net.ParseIP("2001::1"))
	nh = append(nh, net.ParseIP("4.3.2.1").To4())
	testIPsortFail(t, nh)
}

func TestIPsort(t *testing.T) {
	t.Logf("Testing IPv4 address sorting...")
	var nh IPs
	nh = append(nh, net.ParseIP("4.3.2.1"))
	nh = append(nh, net.ParseIP("1.2.3.4"))
	nh = append(nh, net.ParseIP("2.3.4.1"))
	nh = append(nh, net.ParseIP("1.3.2.4"))
	nh = append(nh, net.ParseIP("1.2.3.4"))
	t.Logf("%v", nh)
	sort.Sort(nh)
	for i := 0; i < len(nh)-1; i++ {
		if bytes.Compare(nh[i], nh[i+1]) > 0 {
			t.Errorf("TestIPsort(): %v", nh)
			return
		}
	}
	t.Logf("%v\nconfirmed.", nh)
}

func TestVrf(t *testing.T) {
	var vlanId uint16 = 100
	ifAddrs := []string{"172.16.1.1/24", "172.16.1.2/24"}

	vrf, veth := vrfVethVlanAdd(t, uint16(vlanId))
	testRename(t, &vrf, "vrf01")
	testIpAddrAdd(t, &veth, vlanId, ifAddrs)
	vifName := fmt.Sprintf("%s.%d", veth.Name, vlanId)
	testVrfOf(t, &vrf, vifName)
	testPing(t, &vrf, ifAddrs)
	testRoutes(t, &vrf, "192.168.1.0/24", "172.16.1.3")

	testVlanDelete(t, vifName)
	testVrfDelete(t, &vrf)
	testVethDelete(t, &veth)
}

func TestSetOnlink(t *testing.T) {
	nhi := NHinfo{}
	if err := SetOnlink(&nhi); err != nil {
		t.Fatal(err)
	}
}

func TestVethSetNS(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

}
