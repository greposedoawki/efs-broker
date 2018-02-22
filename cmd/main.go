package main

import (
	"code.cloudfoundry.org/lager"
	"context"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/efs"
	"github.com/pivotal-cf/brokerapi"
	"github.com/satori/go.uuid"
	"github.com/spf13/viper"
	"k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type efsServiceBroker struct {
	client     *efs.EFS
	kubeClient *kubernetes.Clientset
	logger     lager.Logger
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
	creationToken := uuid.NewV4().String()

	//_, err := broker.client.CreateFileSystem(&efs.CreateFileSystemInput{
	//	CreationToken: &uuid,
	//	PerformanceMode: aws.String("generalPurpose"),
	//})
	//if err != nil {
	//	os.Exit(1)
	//
	//fsID := resp.FileSystemId

	configMaps := broker.kubeClient.CoreV1().ConfigMaps("brokers")
	_, err := configMaps.Create(&v1.ConfigMap{
		TypeMeta: meta_v1.TypeMeta{},
		ObjectMeta: meta_v1.ObjectMeta{
			Name: "efs-broker-cm",
		},
		Data: map[string]string{
			"creationToken": creationToken,
		},
		BinaryData: nil,
	})
	if err != nil {
		broker.logger.Error("Failed to create configmap", err)
	}

	return brokerapi.ProvisionedServiceSpec{}, nil
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

func (*efsServiceBroker) LastOperation(ctx context.Context, instanceID, operationData string) (brokerapi.LastOperation, error) {
	panic("implement me")
}

func main() {
	logger := lager.NewLogger("efs-service-broker")
	logger.RegisterSink(lager.NewWriterSink(os.Stdout, lager.DEBUG))
	logger.Info("starting efs-broker")
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

	srv := http.Server{Addr: ":3000"}
	brokerApi := brokerapi.New(efsSB, logger, brokerCredentials)
	http.Handle("/", brokerApi)
	var gracefulStop = make(chan os.Signal)
	signal.Notify(gracefulStop, syscall.SIGTERM)
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
