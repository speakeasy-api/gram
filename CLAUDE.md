# Gram Project Structure Guide

This document provides an overview of the key directories in the Gram project to help you understand the codebase organization.

## Boyd's Law of Iteration

> Speed of iteration beats quality of iteration.

Always consider **Boyd's Law of Iteration** as a guiding principle. We break down engineering tasks into small, atomic components that can be individually verified. 

When working on this codebase:
1. Break problems into smaller atomic parts
2. Build one small part at a time
3. Verify each part independently
4. Integrate verified parts and check they work together

Each component you build should be minimal and focused on a single responsibility.

Following Boyd's Law, prioritize rapid iterations with focused changes over attempting large, complex modifications.

## Key Directories

### infra/helm

Contains Kubernetes Helm charts for deploying Gram:
- `gram/`: Main Helm chart
  - `Chart.yaml`: Chart definition
  - `templates/`: Kubernetes manifest templates
  - `migrations/`: Database migration files
  - `values*.yaml`: Configuration values for different environments

To validate helm charts, run the command `mise helm:validate` 

### infra/terraform

Infrastructure as Code (IaC) configuration:
- `base/`: Core infrastructure resources
  - `dev/`, `prod/`: Environment-specific configs
  - `*.tf`: Terraform configuration files for GKE, Redis, SQL, etc.
- `k8s/`: Kubernetes-specific resources
  - `dev/`, `prod/`: Environment-specific configs
  - `*.tf`: Resources like Atlas, Cert Manager, Ingress, etc.

To validate terraform, run the command `mise helm:gcp:validate`
