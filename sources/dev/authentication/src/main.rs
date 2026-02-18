use std::net::SocketAddr;

use auth_service::config::Config;
use auth_service::AppState;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Load .env
    dotenvy::dotenv().ok();

    // Initialize tracing
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| "auth_service=debug,tower_http=debug".into()),
        )
        .init();

    // Load config
    let config = Config::from_env().expect("Failed to load configuration");

    // Connect to database
    let db = auth_service::db::pool::connect(&config.database_url).await?;
    tracing::info!("Connected to database");

    // Run migrations
    auth_service::db::migration::run(&db).await?;
    tracing::info!("Migrations applied");

    // Check for seed subcommand: cargo run -- seed <email> <password>
    let args: Vec<String> = std::env::args().collect();
    if args.len() > 1 && args[1] == "seed" {
        let email = args
            .get(2)
            .map(|s| s.as_str())
            .unwrap_or("admin@example.com");
        let password = args.get(3).map(|s| s.as_str());

        println!("=== Auth Service Bootstrap ===\n");

        let result = auth_service::seed::bootstrap(&db, email, password).await?;

        println!("  Client ID: {}", result.app_client_id);
        if let Some(ref secret) = result.app_client_secret {
            println!("  Client Secret: {}", secret);
            println!("  (Save this secret — it won't be shown again!)");
        } else {
            println!("  Admin Dashboard application already exists.");
        }
        println!();

        match result.user_action.as_str() {
            "created" => {
                println!("Created admin user: {}", email);
            }
            "promoted" => {
                println!("Promoted {} to admin role.", email);
            }
            "already_admin" => {
                println!("User {} is already an admin.", email);
            }
            _ => {}
        }

        println!("\n=== Bootstrap complete ===");
        println!("\nFor frontend .env, set:");
        println!("  VITE_API_CLIENT_ID={}", result.app_client_id);

        return Ok(());
    }

    // Initialize JWT manager
    let jwt = auth_service::auth::jwt::JwtManager::new(&config)?;

    // Build app state
    let state = AppState {
        db,
        jwt,
        config: config.clone(),
    };

    // Build router
    let app = auth_service::routes::create_router(state);

    // Start server
    let addr: SocketAddr = format!("{}:{}", config.server_host, config.server_port)
        .parse()
        .expect("Invalid server address");

    tracing::info!("Starting server on {addr}");
    let listener = tokio::net::TcpListener::bind(addr).await?;
    axum::serve(listener, app).await?;

    Ok(())
}
