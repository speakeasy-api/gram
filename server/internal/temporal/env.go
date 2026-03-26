package temporal

import "go.temporal.io/sdk/client"

type NamespaceName string
type TaskQueueName string

type Environment struct {
	client    client.Client
	namespace NamespaceName
	queue     TaskQueueName
}

func NewEnvironment(client client.Client, namespace NamespaceName, queue TaskQueueName) *Environment {
	return &Environment{
		client:    client,
		queue:     queue,
		namespace: namespace,
	}
}

func (env *Environment) Client() client.Client {
	return env.client
}

func (env *Environment) Namespace() NamespaceName {
	return env.namespace
}

func (env *Environment) Queue() TaskQueueName {
	return env.queue
}
