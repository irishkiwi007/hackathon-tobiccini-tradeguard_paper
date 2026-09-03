class AppConfig {
  /// Override at build/run time with:
  ///   flutter run --dart-define=POCKETBASE_URL=http://192.168.1.50:8090
  ///
  /// "localhost" only resolves to your own machine from an iOS
  /// simulator. Android emulator needs 10.0.2.2 instead of localhost,
  /// and a physical phone (which is how you'll actually demo this)
  /// needs your machine's LAN IP.
  static const String pocketbaseUrl = String.fromEnvironment(
    'POCKETBASE_URL',
    // defaultValue: 'http://10.0.2.2:8090',
    defaultValue: 'http://localhost:8090',
  );
}
