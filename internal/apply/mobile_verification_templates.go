package apply

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const mobileConfigDart = `const apiBaseUrl = String.fromEnvironment('SMT_API_BASE_URL');

String apiStatusLabel(String value) =>
    value.isEmpty ? 'API not configured' : 'API configured';
`

const mobileMainDart = `import 'package:flutter/material.dart';

import 'src/config.dart';

void main() {
  runApp(const SMTMobileApp());
}

class SMTMobileApp extends StatelessWidget {
  const SMTMobileApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'SMT Mobile',
      home: Scaffold(
        body: Center(
          key: const Key('mobile-home'),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              const Text('SMT Mobile'),
              Text(apiStatusLabel(apiBaseUrl), key: const Key('api-status')),
            ],
          ),
        ),
      ),
    );
  }
}
`

const mobileConfigTestDart = `import 'package:flutter_test/flutter_test.dart';

import 'package:smt_mobile/src/config.dart';

void main() {
  test('the API is optional by default', () {
    expect(apiBaseUrl, isEmpty);
    expect(apiStatusLabel(apiBaseUrl), 'API not configured');
  });

  test('an API endpoint is represented without credentials', () {
    expect(apiStatusLabel('http://127.0.0.1:8080'), 'API configured');
  });
}
`

const mobileWidgetTestDart = `import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:smt_mobile/main.dart';

void main() {
  testWidgets('renders the mobile home and API status hooks', (tester) async {
    await tester.pumpWidget(const SMTMobileApp());

    expect(find.byKey(const Key('mobile-home')), findsOneWidget);
    expect(find.byKey(const Key('api-status')), findsOneWidget);
  });
}
`

const mobileIntegrationTestDart = `import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';

import 'package:smt_mobile/main.dart';

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('launches the mobile contract smoke screen', (tester) async {
    await tester.pumpWidget(const SMTMobileApp());
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('mobile-home')), findsOneWidget);
    expect(find.byKey(const Key('api-status')), findsOneWidget);
  });
}
`

func writeMobileVerificationFiles(root string) error {
	files := map[string]string{
		filepath.Join("lib", "main.dart"):                  mobileMainDart,
		filepath.Join("lib", "src", "config.dart"):         mobileConfigDart,
		filepath.Join("test", "config_test.dart"):          mobileConfigTestDart,
		filepath.Join("test", "widget_test.dart"):          mobileWidgetTestDart,
		filepath.Join("integration_test", "app_test.dart"): mobileIntegrationTestDart,
	}
	for relative, content := range files {
		if err := writeFile(filepath.Join(root, relative), content); err != nil {
			return fmt.Errorf("write %s: %w", relative, err)
		}
	}
	return addIntegrationTestDependency(filepath.Join(root, "pubspec.yaml"))
}

func addIntegrationTestDependency(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Test and integration shims may intentionally emit only platform
			// placeholders. The real Flutter CLI always writes pubspec.yaml;
			// leave those shims untouched while preserving the source contract.
			return nil
		}
		return fmt.Errorf("read pubspec.yaml: %w", err)
	}
	if strings.Contains(string(raw), "  integration_test:\n    sdk: flutter") {
		return nil
	}
	const flutterTest = "  flutter_test:\n    sdk: flutter\n"
	updated := strings.Replace(string(raw), flutterTest, flutterTest+"  integration_test:\n    sdk: flutter\n", 1)
	if updated == string(raw) {
		return fmt.Errorf("pubspec.yaml is missing the flutter_test SDK dependency")
	}
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("write pubspec.yaml: %w", err)
	}
	return nil
}
