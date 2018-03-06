package main

import (
	"code.cloudfoundry.org/lager"
	"context"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/efs"
	"github.com/pivotal-cf/brokerapi"
	"github.com/spf13/viper"
	//"k8s.io/api/core/v1"
	"encoding/json"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
	"gopkg.in/yaml.v2"
)

const (
	efsBrokerCm = "efs-broker-cm"
)

type MountTarget struct {
	IpAddress        string   `json:"ipAddress"`
	Subnet           string   `json:"subnet"`
	ID               string   `json:"id"`
	SecurityGroups   []string `json:"securityGroups,omitempty"`
	AvailabilityZone string   `json:"availabilityZone"`
	EniID            string   `json:"eniID,omitempty"`
}
type EfsConfig struct {
	CreationToken   string            `json:"creationToken"`
	EfsID           string            `json:"efsID"`
	PerformanceMode string            `json:"performanceMode,omitempty"`
	MountTargets    []MountTarget     `json:"mountTargets"`
	Vpc             string            `json:"vpc,omitempty"`
	DnsName         string            `json:"dnsName,omitempty"`
	Encrypted       bool              `json:encrypted,omitempty`
	Tags            map[string]string `json:"tags"`
	Name            string            `json:"name"`
	LifecycleState  string            `json:"lifecycleState"`
}

type EfsBrokerConfig struct {
	FileSystems []EfsConfig `json:"fileSystems"`
}
type efsServiceBroker struct {
	client     *efs.EFS
	kubeClient *kubernetes.Clientset
	logger     lager.Logger
	lock       sync.Mutex
}

func (*efsServiceBroker) plans() map[string]*brokerapi.ServicePlan {
	plans := map[string]*brokerapi.ServicePlan{}
	// only single, shared efs instance is supported

	plans["shared"] = &brokerapi.ServicePlan{
		ID:          "shared-efs",
		Name:        "shared-efs",
		Description: "This plan provides efs resource",
		Metadata: &brokerapi.ServicePlanMetadata{
			DisplayName: "SharedEFS",
		},
	}
	return plans
}

func (broker *efsServiceBroker) Services(ctx context.Context) []brokerapi.Service {
	planList := []brokerapi.ServicePlan{}
	for _, plan := range broker.plans() {
		planList = append(planList, *plan)
	}

	return []brokerapi.Service{
		{
			ID:          "efs-broker-id",
			Name:        "efs-broker-name",
			Description: "EFS Broker Service Description",
			Bindable:    false,
			Plans:       planList,
			Metadata: &brokerapi.ServiceMetadata{
				DisplayName:      "efs-broker-service",
				LongDescription:  "implement me",
				DocumentationUrl: "implement me",
				SupportUrl:       "implement me",
			},
			Tags: []string{
				"aws",
				"efs",
			},
		},
	}

}

func (broker *efsServiceBroker) Provision(ctx context.Context, instanceID string, details brokerapi.ProvisionDetails, asyncAllowed bool) (brokerapi.ProvisionedServiceSpec, error) {
	//panic("implement me")
	configMaps := broker.kubeClient.CoreV1().ConfigMaps("brokers")

	cm, err := configMaps.Get(efsBrokerCm, meta_v1.GetOptions{})
	if err != nil {
		broker.logger.Error("Missing configmap", err)
		return brokerapi.ProvisionedServiceSpec{}, err
	}

	broker.logger.Info("Creating new resource", lager.Data{
		"instanceID": instanceID,
		"details":    details,
	})
	//fs  := cm.Data["fileSystems"]
	fs := EfsBrokerConfig{}
	json.Unmarshal([]byte(cm.Data["fileSystems"]), &fs)
	_, ok := fs[instanceID]
	if !ok {
		instance := &EfsBrokerConfig{}
		fs[instanceID] = instance
	}
	return brokerapi.ProvisionedServiceSpec{
		IsAsync: true,
	}, nil
}

func (broker *efsServiceBroker) Deprovision(ctx context.Context, instanceID string, details brokerapi.DeprovisionDetails, asyncAllowed bool) (brokerapi.DeprovisionServiceSpec, error) {
	configMaps := broker.kubeClient.CoreV1().ConfigMaps("brokers")
	err := configMaps.Delete("efs-broker-cm", &meta_v1.DeleteOptions{})
	if err != nil {
		broker.logger.Error("Failed to delete configmap", err)
	}
	return brokerapi.DeprovisionServiceSpec{}, nil
}

func (*efsServiceBroker) Bind(ctx context.Context, instanceID, bindingID string, details brokerapi.BindDetails) (brokerapi.Binding, error) {
	panic("implement me")
}

func (*efsServiceBroker) Unbind(ctx context.Context, instanceID, bindingID string, details brokerapi.UnbindDetails) error {
	panic("implement me")
}

func (*efsServiceBroker) Update(ctx context.Context, instanceID string, details brokerapi.UpdateDetails, asyncAllowed bool) (brokerapi.UpdateServiceSpec, error) {
	panic("implement me")
}

func (broker *efsServiceBroker) LastOperation(ctx context.Context, instanceID, operationData string) (brokerapi.LastOperation, error) {
	broker.logger.Debug("LastOperation for instance: ", lager.Data{"instanceID": instanceID})
	return brokerapi.LastOperation{
		State:       "",
		Description: "",
	}, nil
}

func (broker *efsServiceBroker) ReadConfig() (EfsConfig, error) {
	configmap := broker.kubeClient.CoreV1().ConfigMaps("brokers")
	cm, err := configmap.Get(efsBrokerCm, meta_v1.GetOptions{})
	if err != nil {
		return EfsConfig{}, err
	}
	config := EfsConfig{}
	yaml.Unmarshal([]byte(cm.Data["config"]), &config )
	return config, nil
}

func (broker *efsServiceBroker) WriteConfig(config EfsConfig) error {
	broker.lock.Lock()
	defer broker.lock.Unlock()
	marshalled, err := yaml.Marshal(&config)
	if err != nil {
		return err
	}
	configmap := broker.kubeClient.CoreV1().ConfigMaps("brokers")
	cm, err := configmap.Get(efsBrokerCm, meta_v1.GetOptions{})
	if err != nil {
		return err
	}
	cm.Data["config"] = string(marshalled)
	configmap.Update(cm)
}

func InitBroker(logger lager.Logger) (*efsServiceBroker, brokerapi.BrokerCredentials) {
	viper.SetConfigName("config.yaml")
	viper.AddConfigPath(".")
	viper.ReadInConfig()
	awsSecret := viper.GetString("awsSecret")
	awsAccess := viper.GetString("awsAccess")
	awsRegion := viper.GetString("awsRegion")
	sess, err := session.NewSession(&aws.Config{
		Region:      aws.String(awsRegion),
		Credentials: credentials.NewStaticCredentials(awsAccess, awsSecret, "123"),
	})
	if err != nil {
		os.Exit(1)
	}
	efsClient := efs.New(sess)
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}
	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	efsSB := &efsServiceBroker{
		client:     efsClient,
		kubeClient: clientSet,
		logger:     logger,
	}
	brokerCredentials := brokerapi.BrokerCredentials{
		Username: "username",
		Password: "password",
	}
	return efsSB, brokerCredentials
}

func main() {
	logger := lager.NewLogger("efs-service-broker")
	logger.RegisterSink(lager.NewWriterSink(os.Stdout, lager.DEBUG))
	logger.Info("starting efs-broker")

	efsSB, brokerCredentials := InitBroker(logger)

	srv := http.Server{Addr: ":3000"}
	brokerApi := brokerapi.New(efsSB, logger, brokerCredentials)
	http.Handle("/", brokerApi)
	var gracefulStop = make(chan os.Signal)
	signal.Notify(gracefulStop, syscall.SIGTERM)
	signal.Notify(gracefulStop, syscall.SIGINT)
	go func() {
		sig := <-gracefulStop
		logger.Info("caught sig: %+v", lager.Data{"signal": sig})
		logger.Info("Wait for 2 second to finish processing")
		srv.Shutdown(nil)
		time.Sleep(2 * time.Second)
		os.Exit(0)
	}()
	srv.ListenAndServe()
}