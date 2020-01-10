/*
Copyright IBM Corp All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package pvtdata

import (
	"path/filepath"
	"syscall"

	"github.com/hyperledger/fabric/integration/nwo"
	"github.com/hyperledger/fabric/integration/nwo/commands"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"
	"github.com/tedsuo/ifrit"
)

func put(network *nwo.Network, peer *nwo.Peer, orderer *nwo.Orderer, key, value string) {
	By("doing a put on " + peer.Name + "." + peer.Organization)
	command := commands.ChaincodeInvoke{
		ChannelID: channelID,
		Orderer:   network.OrdererAddress(orderer, nwo.ListenPort),
		Name:      "pvtdatacc",
		Ctor:      `{"Args":["put","` + key + `","` + value + `"]}`,
		PeerAddresses: []string{
			network.PeerAddress(peer, nwo.ListenPort),
		},
		WaitForEvent: true,
	}
	invokeChaincode(network, peer, command)
}

func query(network *nwo.Network, peer *nwo.Peer, chid, ccid, args string, expectedRv int, errorExpected bool, expectedMsg string) {
	sess, err := network.PeerUserSession(peer, "User1", commands.ChaincodeQuery{
		ChannelID: chid,
		Name:      ccid,
		Ctor:      args,
	})
	Expect(err).NotTo(HaveOccurred())
	Eventually(sess, network.EventuallyTimeout).Should(gexec.Exit(expectedRv))

	if errorExpected {
		Expect(sess.Err).To(gbytes.Say(expectedMsg))
	} else {
		Expect(sess).To(gbytes.Say(expectedMsg))
	}
}

var _ bool = FDescribe("LocalCollections", func() {
	var (
		network *nwo.Network
		process ifrit.Process
		orderer *nwo.Orderer

		org2Peer1      *nwo.Peer
		newPeerProcess ifrit.Process
		cc             chaincode
	)

	BeforeEach(func() {
		org2Peer1 = &nwo.Peer{
			Name:         "peer1",
			Organization: "Org2",
			Channels: []*nwo.PeerChannel{
				{Name: channelID},
			},
		}

		By("setting up the network")
		network = initThreeOrgsSetup()

		network.Bootstrap()
		networkRunner := network.NetworkGroupRunner()
		process = ifrit.Invoke(networkRunner)
		Eventually(process.Ready(), network.EventuallyTimeout).Should(BeClosed())

		orderer = network.Orderer("orderer")
		network.CreateAndJoinChannel(orderer, channelID)
		network.UpdateChannelAnchors(orderer, channelID)

		nwo.EnableCapabilities(network, channelID, "Application", "V2_0", orderer, network.Peers...)
	})

	AfterEach(func() {
		testCleanup(network, process)

		if newPeerProcess != nil {
			newPeerProcess.Signal(syscall.SIGTERM)
			Eventually(newPeerProcess.Wait(), network.EventuallyTimeout).Should(Receive())
		}
	})

	It("Using Local Collections on a chaincode that has collections defined", func() {
		cc = chaincode{
			Chaincode: nwo.Chaincode{
				Name:              "pvtdatacc",
				Version:           "1.0",
				Path:              components.Build("github.com/hyperledger/fabric/integration/chaincode/simplepvtdata/cmd"),
				Lang:              "binary",
				PackageFile:       filepath.Join(network.RootDir, "pvtdata-cc.tar.gz"),
				Label:             "pvtdatacc-label",
				SignaturePolicy:   `OR ('Org1MSP.member','Org2MSP.member', 'Org3MSP.member')`,
				Sequence:          "1",
				CollectionsConfig: filepath.Join("testdata", "collection_configs", "short_btl_config.json"),
			},
		}
		deployChaincode(network, orderer, cc)

		put(network, network.Peer("Org2", "peer0"), orderer, "foo", "bar1")

		By("expecting a successful query return from peer0.Org2")
		query(network, network.Peer("Org2", "peer0"), "testchannel", "pvtdatacc", `{"Args":["get","foo"]}`, 0, false, "bar1")

		By("expecting a failed query return from peer0.Org1")
		query(network, network.Peer("Org1", "peer0"), "testchannel", "pvtdatacc", `{"Args":["get","foo"]}`, 1, true, "private data matching public hash version is not available")

		By("expecting a failed query return from peer0.Org3")
		query(network, network.Peer("Org3", "peer0"), "testchannel", "pvtdatacc", `{"Args":["get","foo"]}`, 1, true, "private data matching public hash version is not available")

		put(network, network.Peer("Org1", "peer0"), orderer, "foo", "bar2")

		By("expecting a successful query return from peer0.Org1")
		query(network, network.Peer("Org1", "peer0"), "testchannel", "pvtdatacc", `{"Args":["get","foo"]}`, 0, false, "bar2")

		By("expecting a failed query return from peer0.Org2")
		query(network, network.Peer("Org2", "peer0"), "testchannel", "pvtdatacc", `{"Args":["get","foo"]}`, 1, true, "private data matching public hash version is not available")

		By("expecting a failed query return from peer0.Org3")
		query(network, network.Peer("Org3", "peer0"), "testchannel", "pvtdatacc", `{"Args":["get","foo"]}`, 1, true, "private data matching public hash version is not available")

		By("doing a put on peer0.Org1 and peer0.Org2")
		sess, err := network.PeerUserSession(network.Peer("Org1", "peer0"), "User1", commands.ChaincodeInvoke{
			ChannelID: "testchannel",
			Orderer:   network.OrdererAddress(orderer, nwo.ListenPort),
			Name:      "pvtdatacc",
			Ctor:      `{"Args":["put","foo","bar3"]}`,
			PeerAddresses: []string{
				network.PeerAddress(network.Peer("Org1", "peer0"), nwo.ListenPort),
				network.PeerAddress(network.Peer("Org2", "peer0"), nwo.ListenPort),
			},

			WaitForEvent: true,
		})
		Expect(err).NotTo(HaveOccurred())
		Eventually(sess, network.EventuallyTimeout).Should(gexec.Exit(0))
		Expect(sess.Err).To(gbytes.Say("Chaincode invoke successful."))

		By("expecting a successful query return from peer0.Org1")
		query(network, network.Peer("Org1", "peer0"), "testchannel", "pvtdatacc", `{"Args":["get","foo"]}`, 0, false, "bar3")

		By("expecting a successful query return from peer0.Org2")
		query(network, network.Peer("Org2", "peer0"), "testchannel", "pvtdatacc", `{"Args":["get","foo"]}`, 0, false, "bar3")

		By("expecting a failed query return from peer0.Org3")
		query(network, network.Peer("Org3", "peer0"), "testchannel", "pvtdatacc", `{"Args":["get","foo"]}`, 1, true, "private data matching public hash version is not available")

		By("peer1.Org2 joins the channel")
		newPeerProcess = addPeer(network, orderer, org2Peer1)
		installChaincode(network, cc, org2Peer1)

		By("make sure all peers have the same ledger height")
		expectedPeers := []*nwo.Peer{
			network.Peer("Org1", "peer0"),
			network.Peer("Org2", "peer0"),
			org2Peer1,
			network.Peer("Org3", "peer0")}
		nwo.WaitUntilEqualLedgerHeight(network, "testchannel", nwo.GetLedgerHeight(network, network.Peer("Org1", "peer0"), "testchannel"), expectedPeers...)

		By("expecting a failed query return from peer1.Org2")
		query(network, org2Peer1, "testchannel", "pvtdatacc", `{"Args":["get","foo"]}`, 1, true, "private data matching public hash version is not available")

		By("doing a put on peer0.Org2 and peer1.Org2")
		sess, err = network.PeerUserSession(network.Peer("Org1", "peer0"), "User1", commands.ChaincodeInvoke{
			ChannelID: "testchannel",
			Orderer:   network.OrdererAddress(orderer, nwo.ListenPort),
			Name:      "pvtdatacc",
			Ctor:      `{"Args":["put","foo","bar4"]}`,
			PeerAddresses: []string{
				network.PeerAddress(network.Peer("Org2", "peer0"), nwo.ListenPort),
				network.PeerAddress(network.Peer("Org2", "peer1"), nwo.ListenPort),
			},

			WaitForEvent: true,
		})
		Expect(err).NotTo(HaveOccurred())
		Eventually(sess, network.EventuallyTimeout).Should(gexec.Exit(0))
		Expect(sess.Err).To(gbytes.Say("Chaincode invoke successful."))

		By("expecting a successful query return from peer0.Org2")
		query(network, network.Peer("Org2", "peer0"), "testchannel", "pvtdatacc", `{"Args":["get","foo"]}`, 0, false, "bar4")

		By("expecting a successful query return from peer1.Org2")
		query(network, network.Peer("Org2", "peer1"), "testchannel", "pvtdatacc", `{"Args":["get","foo"]}`, 0, false, "bar4")

		By("doing a put on peer0.Org2 and peer1.Org2")
		sess, err = network.PeerUserSession(network.Peer("Org1", "peer0"), "User1", commands.ChaincodeInvoke{
			ChannelID: "testchannel",
			Orderer:   network.OrdererAddress(orderer, nwo.ListenPort),
			Name:      "pvtdatacc",
			Ctor:      `{"Args":["put","foo","bar5"]}`,
			PeerAddresses: []string{
				network.PeerAddress(network.Peer("Org2", "peer0"), nwo.ListenPort),
				network.PeerAddress(network.Peer("Org3", "peer0"), nwo.ListenPort),
			},

			WaitForEvent: true,
		})
		Expect(err).NotTo(HaveOccurred())
		Eventually(sess, network.EventuallyTimeout).Should(gexec.Exit(0))
		Expect(sess.Err).To(gbytes.Say("Chaincode invoke successful."))

		By("make sure all peers have the same ledger height")
		expectedPeers = []*nwo.Peer{
			network.Peer("Org1", "peer0"),
			network.Peer("Org2", "peer0"),
			org2Peer1,
			network.Peer("Org3", "peer0")}
		nwo.WaitUntilEqualLedgerHeight(network, "testchannel", nwo.GetLedgerHeight(network, network.Peer("Org1", "peer0"), "testchannel"), expectedPeers...)

		By("expecting a successful query return from peer0.Org2")
		query(network, network.Peer("Org2", "peer0"), "testchannel", "pvtdatacc", `{"Args":["get","foo"]}`, 0, false, "bar5")

		By("expecting a successful query return from peer0.Org3")
		query(network, network.Peer("Org3", "peer0"), "testchannel", "pvtdatacc", `{"Args":["get","foo"]}`, 0, false, "bar5")

		By("expecting a failed query return from peer1.Org2")
		query(network, network.Peer("Org2", "peer1"), "testchannel", "pvtdatacc", `{"Args":["get","foo"]}`, 1, true, "private data matching public hash version is not available")

		By("expecting a failed query return from peer0.Org1")
		query(network, network.Peer("Org1", "peer0"), "testchannel", "pvtdatacc", `{"Args":["get","foo"]}`, 1, true, "private data matching public hash version is not available")
	})

	It("Using Local Collections on a chaincode that has no collections defined", func() {
		cc = chaincode{
			Chaincode: nwo.Chaincode{
				Name:            "pvtdatacc",
				Version:         "1.0",
				Path:            components.Build("github.com/hyperledger/fabric/integration/chaincode/simplepvtdata/cmd"),
				Lang:            "binary",
				PackageFile:     filepath.Join(network.RootDir, "pvtdata-cc.tar.gz"),
				Label:           "pvtdatacc-label",
				SignaturePolicy: `OR ('Org1MSP.member','Org2MSP.member', 'Org3MSP.member')`,
				Sequence:        "1",
			},
		}
		deployChaincode(network, orderer, cc)

		put(network, network.Peer("Org2", "peer0"), orderer, "foo", "bar1")

		By("expecting a successful query return from peer0.Org2")
		query(network, network.Peer("Org2", "peer0"), "testchannel", "pvtdatacc", `{"Args":["get","foo"]}`, 0, false, "bar1")

		By("expecting a failed query return from peer0.Org1")
		query(network, network.Peer("Org1", "peer0"), "testchannel", "pvtdatacc", `{"Args":["get","foo"]}`, 1, true, "private data matching public hash version is not available")

		By("expecting a failed query return from peer0.Org3")
		query(network, network.Peer("Org3", "peer0"), "testchannel", "pvtdatacc", `{"Args":["get","foo"]}`, 1, true, "private data matching public hash version is not available")

		put(network, network.Peer("Org1", "peer0"), orderer, "foo", "bar2")

		By("expecting a successful query return from peer0.Org1")
		query(network, network.Peer("Org1", "peer0"), "testchannel", "pvtdatacc", `{"Args":["get","foo"]}`, 0, false, "bar2")

		By("expecting a failed query return from peer0.Org2")
		query(network, network.Peer("Org2", "peer0"), "testchannel", "pvtdatacc", `{"Args":["get","foo"]}`, 1, true, "private data matching public hash version is not available")

		By("expecting a failed query return from peer0.Org3")
		query(network, network.Peer("Org3", "peer0"), "testchannel", "pvtdatacc", `{"Args":["get","foo"]}`, 1, true, "private data matching public hash version is not available")

		By("doing a put on peer0.Org1 and peer0.Org2")
		sess, err := network.PeerUserSession(network.Peer("Org1", "peer0"), "User1", commands.ChaincodeInvoke{
			ChannelID: "testchannel",
			Orderer:   network.OrdererAddress(orderer, nwo.ListenPort),
			Name:      "pvtdatacc",
			Ctor:      `{"Args":["put","foo","bar3"]}`,
			PeerAddresses: []string{
				network.PeerAddress(network.Peer("Org1", "peer0"), nwo.ListenPort),
				network.PeerAddress(network.Peer("Org2", "peer0"), nwo.ListenPort),
			},

			WaitForEvent: true,
		})
		Expect(err).NotTo(HaveOccurred())
		Eventually(sess, network.EventuallyTimeout).Should(gexec.Exit(0))
		Expect(sess.Err).To(gbytes.Say("Chaincode invoke successful."))

		By("expecting a successful query return from peer0.Org1")
		query(network, network.Peer("Org1", "peer0"), "testchannel", "pvtdatacc", `{"Args":["get","foo"]}`, 0, false, "bar3")

		By("expecting a successful query return from peer0.Org2")
		query(network, network.Peer("Org2", "peer0"), "testchannel", "pvtdatacc", `{"Args":["get","foo"]}`, 0, false, "bar3")

		By("expecting a failed query return from peer0.Org3")
		query(network, network.Peer("Org3", "peer0"), "testchannel", "pvtdatacc", `{"Args":["get","foo"]}`, 1, true, "private data matching public hash version is not available")

		By("peer1.Org2 joins the channel")
		newPeerProcess = addPeer(network, orderer, org2Peer1)
		installChaincode(network, cc, org2Peer1)

		By("make sure all peers have the same ledger height")
		expectedPeers := []*nwo.Peer{
			network.Peer("Org1", "peer0"),
			network.Peer("Org2", "peer0"),
			org2Peer1,
			network.Peer("Org3", "peer0")}
		nwo.WaitUntilEqualLedgerHeight(network, "testchannel", nwo.GetLedgerHeight(network, network.Peer("Org1", "peer0"), "testchannel"), expectedPeers...)

		By("expecting a failed query return from peer1.Org2")
		query(network, org2Peer1, "testchannel", "pvtdatacc", `{"Args":["get","foo"]}`, 1, true, "private data matching public hash version is not available")

		By("doing a put on peer0.Org2 and peer1.Org2")
		sess, err = network.PeerUserSession(network.Peer("Org1", "peer0"), "User1", commands.ChaincodeInvoke{
			ChannelID: "testchannel",
			Orderer:   network.OrdererAddress(orderer, nwo.ListenPort),
			Name:      "pvtdatacc",
			Ctor:      `{"Args":["put","foo","bar4"]}`,
			PeerAddresses: []string{
				network.PeerAddress(network.Peer("Org2", "peer0"), nwo.ListenPort),
				network.PeerAddress(network.Peer("Org2", "peer1"), nwo.ListenPort),
			},

			WaitForEvent: true,
		})
		Expect(err).NotTo(HaveOccurred())
		Eventually(sess, network.EventuallyTimeout).Should(gexec.Exit(0))
		Expect(sess.Err).To(gbytes.Say("Chaincode invoke successful."))

		By("expecting a successful query return from peer0.Org2")
		query(network, network.Peer("Org2", "peer0"), "testchannel", "pvtdatacc", `{"Args":["get","foo"]}`, 0, false, "bar4")

		By("expecting a successful query return from peer1.Org2")
		query(network, network.Peer("Org2", "peer1"), "testchannel", "pvtdatacc", `{"Args":["get","foo"]}`, 0, false, "bar4")

		By("doing a put on peer0.Org2 and peer1.Org2")
		sess, err = network.PeerUserSession(network.Peer("Org1", "peer0"), "User1", commands.ChaincodeInvoke{
			ChannelID: "testchannel",
			Orderer:   network.OrdererAddress(orderer, nwo.ListenPort),
			Name:      "pvtdatacc",
			Ctor:      `{"Args":["put","foo","bar5"]}`,
			PeerAddresses: []string{
				network.PeerAddress(network.Peer("Org2", "peer0"), nwo.ListenPort),
				network.PeerAddress(network.Peer("Org3", "peer0"), nwo.ListenPort),
			},

			WaitForEvent: true,
		})
		Expect(err).NotTo(HaveOccurred())
		Eventually(sess, network.EventuallyTimeout).Should(gexec.Exit(0))
		Expect(sess.Err).To(gbytes.Say("Chaincode invoke successful."))

		By("make sure all peers have the same ledger height")
		expectedPeers = []*nwo.Peer{
			network.Peer("Org1", "peer0"),
			network.Peer("Org2", "peer0"),
			org2Peer1,
			network.Peer("Org3", "peer0")}
		nwo.WaitUntilEqualLedgerHeight(network, "testchannel", nwo.GetLedgerHeight(network, network.Peer("Org1", "peer0"), "testchannel"), expectedPeers...)

		By("expecting a successful query return from peer0.Org2")
		query(network, network.Peer("Org2", "peer0"), "testchannel", "pvtdatacc", `{"Args":["get","foo"]}`, 0, false, "bar5")

		By("expecting a successful query return from peer0.Org3")
		query(network, network.Peer("Org3", "peer0"), "testchannel", "pvtdatacc", `{"Args":["get","foo"]}`, 0, false, "bar5")

		By("expecting a failed query return from peer1.Org2")
		query(network, network.Peer("Org2", "peer1"), "testchannel", "pvtdatacc", `{"Args":["get","foo"]}`, 1, true, "private data matching public hash version is not available")

		By("expecting a failed query return from peer0.Org1")
		query(network, network.Peer("Org1", "peer0"), "testchannel", "pvtdatacc", `{"Args":["get","foo"]}`, 1, true, "private data matching public hash version is not available")
	})
})
