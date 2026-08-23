package zitadel

import (
	"os"
	"strings"
	"testing"
)

func TestFoundationDeclaresTheSingleHostZitadelContract(t *testing.T) {
	main, err := os.ReadFile("main.tf")
	if err != nil {
		t.Fatal(err)
	}
	contents := string(main)
	for _, marker := range []string{
		"aws_vpc",
		"aws_subnet",
		"aws_nat_gateway",
		"aws_lb",
		"protocol_version = \"HTTP2\"",
		"/debug/ready",
		"aws_autoscaling_group",
		"desired_capacity",
		"aws_db_instance",
		"multi_az",
		"aws_secretsmanager_secret",
		"aws_kms_key",
		"AmazonSSMManagedInstanceCore",
		"aws_cloudwatch_metric_alarm",
	} {
		if !strings.Contains(contents, marker) {
			t.Fatalf("main.tf missing contract marker %q", marker)
		}
	}
}

func TestFoundationDoesNotEmbedRuntimeSecrets(t *testing.T) {
	for _, name := range []string{"main.tf", "variables.tf", "outputs.tf", "README.md", "terraform.tfvars.example"} {
		contents, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{
			"smt-zitadel-password",
			"smt-zitadel-masterkey",
			"secret_string =",
			"password = \"",
		} {
			if strings.Contains(string(contents), forbidden) {
				t.Fatalf("%s embeds forbidden runtime secret marker %q", name, forbidden)
			}
		}
	}
}
