/*
 * Copyright 2018, CS Systemes d'Information, http://www.c-s.fr
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package flexibleengine

import (
	"fmt"
	"net/url"

	"github.com/CS-SI/SafeScale/utils/metadata"

	"github.com/CS-SI/SafeScale/providers"
	"github.com/CS-SI/SafeScale/providers/api"
	"github.com/CS-SI/SafeScale/providers/enums/VolumeSpeed"
	"github.com/CS-SI/SafeScale/providers/openstack"

	// Gophercloud OpenStack API
	gc "github.com/gophercloud/gophercloud"
	gcos "github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/identity/v3/projects"
	"github.com/gophercloud/gophercloud/openstack/identity/v3/tokens"
	secgroups "github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/security/groups"
	secrules "github.com/gophercloud/gophercloud/openstack/networking/v2/extensions/security/rules"
	"github.com/gophercloud/gophercloud/pagination"

	// official AWS API
	"github.com/aws/aws-sdk-go/aws"
	awscreds "github.com/aws/aws-sdk-go/aws/credentials"
	awssession "github.com/aws/aws-sdk-go/aws/session"
)

// AuthOptions fields are the union of those recognized by each identity implementation and
// provider.
type AuthOptions struct {
	IdentityEndpoint string
	Username         string
	Password         string
	DomainName       string
	ProjectID        string

	AllowReauth bool

	// TokenID allows users to authenticate (possibly as another user) with an
	// authentication token ID.
	TokenID string

	//Openstack region (data center) where the infrstructure will be created
	Region string

	//FloatingIPPool name of the floating IP pool
	//Necessary only if UseFloatingIP is true
	//FloatingIPPool string

	// Name of the VPC (Virtual Private Cloud)
	VPCName string
	// CIDR if the VPC
	VPCCIDR string

	// Identifier for S3 object storage use
	S3AccessKeyID string
	// Password of the previous identifier
	S3AccessKeyPassword string
}

const (
	defaultUser string = "cloud"

	authURL string = "https://iam.%s.prod-cloud-ocb.orange-business.com"
)

//VPL:BEGIN
// aws provider isn't finished yet, copying the necessary here meanwhile...

// AuthOpts AWS credentials
type awsAuthOpts struct {
	// AWS Access key ID
	AccessKeyID string

	// AWS Secret Access Key
	SecretAccessKey string
	// The region to send requests to. This parameter is required and must
	// be configured globally or on a per-client basis unless otherwise
	// noted. A full list of regions is found in the "Regions and Endpoints"
	// document.
	//
	// @see http://docs.aws.amazon.com/general/latest/gr/rande.html
	//   AWS Regions and Endpoints
	Region string
	//Config *Config
}

// Retrieve returns nil if it successfully retrieved the value.
// Error is returned if the value were not obtainable, or empty.
func (o awsAuthOpts) Retrieve() (awscreds.Value, error) {
	return awscreds.Value{
		AccessKeyID:     o.AccessKeyID,
		SecretAccessKey: o.SecretAccessKey,
		ProviderName:    "internal",
	}, nil
}

// IsExpired returns if the credentials are no longer valid, and need
// to be retrieved.
func (o awsAuthOpts) IsExpired() bool {
	return false
}

//VPL:END

// AuthenticatedClient returns an authenticated client
func AuthenticatedClient(opts AuthOptions, cfg openstack.CfgOptions) (*Client, error) {
	// gophercloud doesn't know how to determine Auth API version to use for FlexibleEngine.
	// So we help him to.
	if opts.IdentityEndpoint == "" {
		opts.IdentityEndpoint = fmt.Sprintf(authURL, opts.Region)
	}
	provider, err := gcos.NewClient(opts.IdentityEndpoint)
	if err != nil {
		return nil, err
	}
	authOptions := tokens.AuthOptions{
		IdentityEndpoint: opts.IdentityEndpoint,
		Username:         opts.Username,
		Password:         opts.Password,
		DomainName:       opts.DomainName,
		AllowReauth:      opts.AllowReauth,
		Scope: tokens.Scope{
			ProjectName: opts.Region,
			DomainName:  opts.DomainName,
		},
	}
	err = gcos.AuthenticateV3(provider, &authOptions, gc.EndpointOpts{})
	if err != nil {
		return nil, fmt.Errorf("%s", openstack.ProviderErrorToString(err))
	}

	// Identity API
	identity, err := gcos.NewIdentityV3(provider, gc.EndpointOpts{})
	if err != nil {
		return nil, fmt.Errorf("%s", openstack.ProviderErrorToString(err))
	}

	// Recover Project ID of region
	listOpts := projects.ListOpts{
		Enabled: gc.Enabled,
		Name:    opts.Region,
	}
	allPages, err := projects.List(identity, listOpts).AllPages()
	if err != nil {
		return nil, fmt.Errorf("failed to query project ID corresponding to region '%s': %s", opts.Region, openstack.ProviderErrorToString(err))
	}
	allProjects, err := projects.ExtractProjects(allPages)
	if err != nil {
		return nil, fmt.Errorf("failed to load project ID corresponding to region '%s': %s", opts.Region, openstack.ProviderErrorToString(err))
	}
	if len(allProjects) > 0 {
		opts.ProjectID = allProjects[0].ID
	} else {
		return nil, fmt.Errorf("failed to found project ID corresponding to region '%s': %s", opts.Region, openstack.ProviderErrorToString(err))
	}

	// Compute API
	compute, err := gcos.NewComputeV2(provider, gc.EndpointOpts{})
	if err != nil {
		return nil, fmt.Errorf("%s", openstack.ProviderErrorToString(err))
	}

	// Network API
	network, err := gcos.NewNetworkV2(provider, gc.EndpointOpts{
		Type:   "network",
		Region: opts.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("%s", openstack.ProviderErrorToString(err))
	}

	// Storage API
	blockStorage, err := gcos.NewBlockStorageV2(provider, gc.EndpointOpts{
		Type:   "volumev2",
		Region: opts.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("%s", openstack.ProviderErrorToString(err))
	}

	// Need to get Endpoint URL for ObjectStorage, that will be used with AWS S3 protocol
	objectStorage, err := gcos.NewObjectStorageV1(provider, gc.EndpointOpts{
		Type:   "object",
		Region: opts.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("%s", openstack.ProviderErrorToString(err))
	}
	// Fix URL of ObjectStorage for FlexibleEngine...
	u, _ := url.Parse(objectStorage.Endpoint)
	endpoint := u.Scheme + "://" + u.Hostname() + "/"
	// FlexibleEngine uses a protocol compatible with S3, so we need to get aws.Session instance
	authOpts := awsAuthOpts{
		AccessKeyID:     opts.S3AccessKeyID,
		SecretAccessKey: opts.S3AccessKeyPassword,
		Region:          opts.Region,
	}
	awsSession, err := awssession.NewSession(&aws.Config{
		Region:      aws.String(opts.Region),
		Credentials: awscreds.NewCredentials(authOpts),
		Endpoint:    &endpoint,
	})
	if err != nil {
		return nil, err
	}
	openstackClient := openstack.Client{
		Opts: &openstack.AuthOptions{
			IdentityEndpoint: opts.IdentityEndpoint,
			Username:         opts.Username,
			Password:         opts.Password,
			DomainName:       opts.DomainName,
			AllowReauth:      opts.AllowReauth,
			Region:           opts.Region,
		},
		Cfg: &openstack.CfgOptions{
			DNSList:             cfg.DNSList,
			UseFloatingIP:       true,
			UseLayer3Networking: cfg.UseLayer3Networking,
			VolumeSpeeds:        cfg.VolumeSpeeds,
			S3Protocol:          "s3",
			MetadataBucketName:  api.BuildMetadataBucketName(opts.DomainName),
		},
		Provider: provider,
		Compute:  compute,
		Network:  network,
		Volume:   blockStorage,
		//Container:   objectStorage,
	}

	clt := Client{
		Opts:      &opts,
		osclt:     &openstackClient,
		Identity:  identity,
		S3Session: awsSession,
	}

	// Initializes the VPC
	err = clt.initVPC()
	if err != nil {
		return nil, err
	}

	// Initializes the default security group
	err = clt.initDefaultSecurityGroup()
	if err != nil {
		return nil, err
	}

	// Creates metadata Object Storage container
	err = metadata.InitializeBucket(&clt)
	if err != nil {
		return nil, err
	}
	return &clt, nil
}

//Client is the implementation of the flexibleengine driver regarding to the api.ClientAPI
type Client struct {
	// Opts contains authentication options
	Opts *AuthOptions
	// Identity contains service client of Identity openstack service
	Identity *gc.ServiceClient
	// S3Session is the "AWS Session" for object storage use (compatible S3)
	S3Session *awssession.Session
	// osclt is the openstack.Client instance to use when fully openstack compliant
	osclt *openstack.Client
	// Instance of the VPC
	vpc *VPC
	// defaultSecurityGroup contains the name of the default security group for the VPC
	defaultSecurityGroup string
	// SecurityGroup is an instance of the default security group
	SecurityGroup *secgroups.SecGroup
}

// Build build a new Client from configuration parameter
func (client *Client) Build(params map[string]interface{}) (api.ClientAPI, error) {
	Username, _ := params["Username"].(string)
	Password, _ := params["Password"].(string)
	DomainName, _ := params["DomainName"].(string)
	ProjectID, _ := params["ProjectID"].(string)
	VPCName, _ := params["VPCName"].(string)
	VPCCIDR, _ := params["VPCCIDR"].(string)
	Region, _ := params["Region"].(string)
	S3AccessKeyID, _ := params["S3AccessKeyID"].(string)
	S3AccessKeyPassword, _ := params["S3AccessKeyPassword"].(string)

	return AuthenticatedClient(AuthOptions{
		Username:            Username,
		Password:            Password,
		DomainName:          DomainName,
		ProjectID:           ProjectID,
		Region:              Region,
		AllowReauth:         true,
		VPCName:             VPCName,
		VPCCIDR:             VPCCIDR,
		S3AccessKeyID:       S3AccessKeyID,
		S3AccessKeyPassword: S3AccessKeyPassword,
	}, openstack.CfgOptions{
		DNSList:             []string{"100.125.0.41", "100.126.0.41"},
		UseFloatingIP:       true,
		UseLayer3Networking: false,
		VolumeSpeeds: map[string]VolumeSpeed.Enum{
			"SATA": VolumeSpeed.COLD,
			"SSD":  VolumeSpeed.SSD,
		},
	})
}

/*
 * VPL: Duplicated code from providers/openstack/client.go
 *      Because of the concept of VPC in FlexibleEngine, we need to create
 *      default Security Group bound to a VPC, to prevent side effect if
 *      a default Security Group is changed
 */

// getDefaultSecurityGroup returns the default security group for the client, in the form
// sg-<VPCName>, if it exists.
func (client *Client) getDefaultSecurityGroup() (*secgroups.SecGroup, error) {
	var sgList []secgroups.SecGroup
	opts := secgroups.ListOpts{
		Name: client.defaultSecurityGroup,
	}
	err := secgroups.List(client.osclt.Network, opts).EachPage(func(page pagination.Page) (bool, error) {
		list, err := secgroups.ExtractGroups(page)
		if err != nil {
			return false, err
		}
		for _, g := range list {
			if g.Name == client.defaultSecurityGroup {
				sgList = append(sgList, g)
			}
		}
		return true, nil
	})
	if err != nil {
		return nil, fmt.Errorf("Error listing routers: %s", openstack.ProviderErrorToString(err))
	}
	if len(sgList) == 0 {
		return nil, nil
	}
	if len(sgList) > 1 {
		return nil, fmt.Errorf("Configuration error: multiple Security Groups named '%s' exist", client.defaultSecurityGroup)
	}

	return &sgList[0], nil
}

// createTCPRules creates TCP rules to configure the default security group
func (client *Client) createTCPRules(groupID string) error {
	// Inbound == ingress == coming from Outside
	ruleOpts := secrules.CreateOpts{
		Direction:      secrules.DirIngress,
		PortRangeMin:   1,
		PortRangeMax:   65535,
		EtherType:      secrules.EtherType4,
		SecGroupID:     groupID,
		Protocol:       secrules.ProtocolTCP,
		RemoteIPPrefix: "0.0.0.0/0",
	}
	_, err := secrules.Create(client.osclt.Network, ruleOpts).Extract()
	if err != nil {
		return err
	}
	ruleOpts = secrules.CreateOpts{
		Direction:      secrules.DirIngress,
		PortRangeMin:   1,
		PortRangeMax:   65535,
		EtherType:      secrules.EtherType6,
		SecGroupID:     groupID,
		Protocol:       secrules.ProtocolTCP,
		RemoteIPPrefix: "::/0",
	}
	_, err = secrules.Create(client.osclt.Network, ruleOpts).Extract()
	if err != nil {
		return err
	}

	// Outbound = egress == going to Outside
	ruleOpts = secrules.CreateOpts{
		Direction:      secrules.DirEgress,
		PortRangeMin:   1,
		PortRangeMax:   65535,
		EtherType:      secrules.EtherType4,
		SecGroupID:     groupID,
		Protocol:       secrules.ProtocolTCP,
		RemoteIPPrefix: "0.0.0.0/0",
	}
	_, err = secrules.Create(client.osclt.Network, ruleOpts).Extract()
	if err != nil {
		return err
	}
	ruleOpts = secrules.CreateOpts{
		Direction:      secrules.DirEgress,
		PortRangeMin:   1,
		PortRangeMax:   65535,
		EtherType:      secrules.EtherType6,
		SecGroupID:     groupID,
		Protocol:       secrules.ProtocolTCP,
		RemoteIPPrefix: "::/0",
	}
	_, err = secrules.Create(client.osclt.Network, ruleOpts).Extract()
	return err
}

// createTCPRules creates UDP rules to configure the default security group
func (client *Client) createUDPRules(groupID string) error {
	// Inbound == ingress == coming from Outside
	ruleOpts := secrules.CreateOpts{
		Direction:      secrules.DirIngress,
		PortRangeMin:   1,
		PortRangeMax:   65535,
		EtherType:      secrules.EtherType4,
		SecGroupID:     groupID,
		Protocol:       secrules.ProtocolUDP,
		RemoteIPPrefix: "0.0.0.0/0",
	}
	_, err := secrules.Create(client.osclt.Network, ruleOpts).Extract()
	if err != nil {
		return err
	}
	ruleOpts = secrules.CreateOpts{
		Direction:      secrules.DirIngress,
		PortRangeMin:   1,
		PortRangeMax:   65535,
		EtherType:      secrules.EtherType6,
		SecGroupID:     groupID,
		Protocol:       secrules.ProtocolUDP,
		RemoteIPPrefix: "::/0",
	}
	_, err = secrules.Create(client.osclt.Network, ruleOpts).Extract()
	if err != nil {
		return err
	}

	// Outbound = egress == going to Outside
	ruleOpts = secrules.CreateOpts{
		Direction:      secrules.DirEgress,
		PortRangeMin:   1,
		PortRangeMax:   65535,
		EtherType:      secrules.EtherType4,
		SecGroupID:     groupID,
		Protocol:       secrules.ProtocolUDP,
		RemoteIPPrefix: "0.0.0.0/0",
	}
	_, err = secrules.Create(client.osclt.Network, ruleOpts).Extract()
	if err != nil {
		return err
	}
	ruleOpts = secrules.CreateOpts{
		Direction:      secrules.DirEgress,
		PortRangeMin:   1,
		PortRangeMax:   65535,
		EtherType:      secrules.EtherType6,
		SecGroupID:     groupID,
		Protocol:       secrules.ProtocolUDP,
		RemoteIPPrefix: "::/0",
	}
	_, err = secrules.Create(client.osclt.Network, ruleOpts).Extract()
	return err
}

// createICMPRules creates UDP rules to configure the default security group
func (client *Client) createICMPRules(groupID string) error {
	// Inbound == ingress == coming from Outside
	ruleOpts := secrules.CreateOpts{
		Direction:      secrules.DirIngress,
		EtherType:      secrules.EtherType4,
		SecGroupID:     groupID,
		Protocol:       secrules.ProtocolICMP,
		RemoteIPPrefix: "0.0.0.0/0",
	}
	_, err := secrules.Create(client.osclt.Network, ruleOpts).Extract()
	if err != nil {
		return err
	}
	ruleOpts = secrules.CreateOpts{
		Direction: secrules.DirIngress,
		//		PortRangeMin:   0,
		//		PortRangeMax:   18,
		EtherType:      secrules.EtherType6,
		SecGroupID:     groupID,
		Protocol:       secrules.ProtocolICMP,
		RemoteIPPrefix: "::/0",
	}
	_, err = secrules.Create(client.osclt.Network, ruleOpts).Extract()
	if err != nil {
		return err
	}

	// Outbound = egress == going to Outside
	ruleOpts = secrules.CreateOpts{
		Direction: secrules.DirEgress,
		//		PortRangeMin:   0,
		//		PortRangeMax:   18,
		EtherType:      secrules.EtherType4,
		SecGroupID:     groupID,
		Protocol:       secrules.ProtocolICMP,
		RemoteIPPrefix: "0.0.0.0/0",
	}
	_, err = secrules.Create(client.osclt.Network, ruleOpts).Extract()
	if err != nil {
		return err
	}
	ruleOpts = secrules.CreateOpts{
		Direction: secrules.DirEgress,
		//		PortRangeMin:   0,
		//		PortRangeMax:   18,
		EtherType:      secrules.EtherType6,
		SecGroupID:     groupID,
		Protocol:       secrules.ProtocolICMP,
		RemoteIPPrefix: "::/0",
	}
	_, err = secrules.Create(client.osclt.Network, ruleOpts).Extract()
	return err
}

// initDefaultSecurityGroup create an open Security Group
// The default security group opens all TCP, UDP, ICMP ports
// Security is managed individually on each host using a linux firewall
func (client *Client) initDefaultSecurityGroup() error {
	client.defaultSecurityGroup = "sg-" + client.Opts.VPCName

	sg, err := client.getDefaultSecurityGroup()
	if err != nil {
		return err
	}
	if sg != nil {
		client.SecurityGroup = sg
		return nil
	}
	opts := secgroups.CreateOpts{
		Name:        client.defaultSecurityGroup,
		Description: "Default security group for VPC " + client.Opts.VPCName,
	}
	group, err := secgroups.Create(client.osclt.Network, opts).Extract()
	if err != nil {
		return fmt.Errorf("Failed to create Security Group '%s': %s", client.defaultSecurityGroup, openstack.ProviderErrorToString(err))
	}
	err = client.createTCPRules(group.ID)
	if err == nil {
		err = client.createUDPRules(group.ID)
		if err == nil {
			err = client.createICMPRules(group.ID)
			if err == nil {
				client.SecurityGroup = group
				return nil
			}
		}
	}
	// Error occured...
	secgroups.Delete(client.osclt.Network, group.ID)
	return err
}

// initVPC initializes the VPC if it doesn't exist
func (client *Client) initVPC() error {
	// Tries to get VPC information
	vpcID, err := client.findVPCID()
	if err != nil {
		return err
	}
	if vpcID != nil {
		client.vpc, err = client.GetVPC(*vpcID)
		return err
	}

	vpc, err := client.CreateVPC(VPCRequest{
		Name: client.Opts.VPCName,
		CIDR: client.Opts.VPCCIDR,
	})
	if err != nil {
		return fmt.Errorf("Failed to initialize VPC '%s': %s", client.Opts.VPCName, openstack.ProviderErrorToString(err))
	}
	client.vpc = vpc
	return nil
}

// findVPC returns the ID about the VPC
func (client *Client) findVPCID() (*string, error) {
	var router *openstack.Router
	found := false
	routers, err := client.osclt.ListRouters()
	if err != nil {
		return nil, fmt.Errorf("Error listing routers: %s", openstack.ProviderErrorToString(err))
	}
	for _, r := range routers {
		if r.Name == client.Opts.VPCName {
			found = true
			router = &r
			break
		}
	}
	if found {
		return &router.ID, nil
	}
	return nil, nil
}

// GetAuthOpts returns the auth options
func (client *Client) GetAuthOpts() (api.Config, error) {
	cfg := api.ConfigMap{}

	cfg.Set("DomainName", client.Opts.DomainName)
	cfg.Set("Login", client.Opts.Username)
	cfg.Set("Password", client.Opts.Password)
	cfg.Set("AuthUrl", client.Opts.IdentityEndpoint)
	cfg.Set("Region", client.Opts.Region)
	cfg.Set("VPCName", client.Opts.VPCName)

	return cfg, nil
}

// GetCfgOpts return configuration parameters
func (client *Client) GetCfgOpts() (api.Config, error) {
	return client.osclt.GetCfgOpts()
}

// init registers the flexibleengine provider
func init() {
	providers.Register("flexibleengine", &Client{})
}
