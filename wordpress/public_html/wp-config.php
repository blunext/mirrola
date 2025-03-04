<?php
/**
 * The base configurations of the WordPress.
 *
 * This file has the following configurations: MySQL settings, Table Prefix,
 * Secret Keys, WordPress Language, and ABSPATH. You can find more information
 * by visiting {@link http://codex.wordpress.org/Editing_wp-config.php Editing
 * wp-config.php} Codex page. You can get the MySQL settings from your web host.
 *
 * This file is used by the wp-config.php creation script during the
 * installation. You don't have to use the web site, you can just copy this file
 * to "wp-config.php" and fill in the values.
 *
 * @package WordPress
 */

// ** MySQL settings - You can get this info from your web host ** //
/** The name of the database for WordPress */
// define('WP_CACHE', true); //Added by WP-Cache Manager
// define( 'WPCACHEHOME', '/home/olamundo/domains/olamundo.pl/public_html/wp-content/plugins/wp-super-cache/' ); //Added by WP-Cache Manager

define('DB_NAME', 'wordpress');

/** MySQL database username */
define('DB_USER', 'wordpress');


/** MySQL database password */
define('DB_PASSWORD', 'wordpress');

/** MySQL hostname */
define('DB_HOST', getenv('WORDPRESS_DB_HOST'));

/** Database Charset to use in creating database tables. */
define('DB_CHARSET', 'utf8');

/** The Database Collate type. Don't change this if in doubt. */
define('DB_COLLATE', '');

/** logowanie i admin przez ssl */
// define('FORCE_SSL_LOGIN', true);
// define('FORCE_SSL_ADMIN', true);


/**#@+
 * Authentication Unique Keys and Salts.
 *
 * Change these to different unique phrases!
 * You can generate these using the {@link https://api.wordpress.org/secret-key/1.1/salt/ WordPress.org secret-key service}
 * You can change these at any point in time to invalidate all existing cookies. This will force all users to have to log in again.
 *
 * @since 2.6.0
 */
define('AUTH_KEY',         'y16Joy2FP0fcTnvnC7kVwQGWKc5W0WbouFK2fhNv7uFCHJ6srl8px6yOdWX4M5Vg');
define('SECURE_AUTH_KEY',  'pzxmgRKoiixAb5VqcXTMeoxmE79NPJm3QOBI1JDC06AmkyuVCbWw1lBGRohuEnfY');
define('LOGGED_IN_KEY',    'SrWpRGkgngYY3KL0zE9qNzWPamRQEz1CTJ8iveznr0Vjofj00kQMSfruRdGsDw5b');
define('NONCE_KEY',        'EjWPke8c9yKWxSM1MYRjtltUbJFY4MPcnF657OtJpwISuo3meD7hDnmTrBdttiU7');
define('AUTH_SALT',        'uIVP13QA5oI2Tv8JOmZqCBzDWk5033AgG8l83fhgCtRAkJ14ze1K682mNHTteldv');
define('SECURE_AUTH_SALT', 'pXAQp1PS6HvPxMbhlETP5zpnxXteW9cv3GXIyW2AsFpI5nJbB3zgYaHIv6O1KVAu');
define('LOGGED_IN_SALT',   'omYrBIvKP4MMOtvXVXNZxbOjWXfBcLmVLfcMd6wxSl0F7TGyHATwlm8bc80p4uYX');
define('NONCE_SALT',       'DliXlSaw3JZuKX1Q7eBoJSfTpCWypa5tE4ur5l8SGPvVknp6Fc0gbAEoQjEz9rAV');

/**
 * Other customizations.
 */
define('FS_METHOD','direct');define('FS_CHMOD_DIR',0755);define('FS_CHMOD_FILE',0644);
define('WP_TEMP_DIR',dirname(__FILE__).'/wp-content/uploads');

/**
 * Turn off automatic updates since these are managed upstream.
 */
define('AUTOMATIC_UPDATER_DISABLED', true);


/**#@-*/

/**
 * WordPress Database Table prefix.
 *
 * You can have multiple installations in one database if you give each a unique
 * prefix. Only numbers, letters, and underscores please!
 */
$table_prefix  = 'wp_';

/**
 * WordPress Localized Language, defaults to English.
 *
 * Change this to localize WordPress. A corresponding MO file for the chosen
 * language must be installed to wp-content/languages. For example, install
 * de_DE.mo to wp-content/languages and set WPLANG to 'de_DE' to enable German
 * language support.
 */
define('WPLANG', 'pl_PL');

/**
 * For developers: WordPress debugging mode.
 *
 * Change this to true to enable the display of notices during development.
 * It is strongly recommended that plugin and theme developers use WP_DEBUG
 * in their development environments.
 */
define('WP_DEBUG', true);

/* That's all, stop editing! Happy blogging. */

/** Absolute path to the WordPress directory. */
if ( !defined('ABSPATH') )
	define('ABSPATH', dirname(__FILE__) . '/');

/** Sets up WordPress vars and included files. */
require_once(ABSPATH . 'wp-settings.php');
