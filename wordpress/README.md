Get DB and WordPress from server

```bash
ssh om
```

```bash
tar -czvf wordpress.tar.gz --exclude='./public_html/wp-content/cache/*' ./public_html/*
cat public_html/wp-config.php | grep DB_
mysqldump -u olamundo_wp1 -p olamundo_wp1 > wordpress_db.sql
exit
```

```bash
scp om:wordpress.tar.gz .
scp om:wordpress_db.sql .
tar -xzvf wordpress.tar.gz

sed -i 's|/https://olamundo.pl|http://olamundo:8000|g' wordpress_db.sql
sed -i 's|/http://olamundo.pl|http://olamundo:8000|g' wordpress_db.sql
sed -i 's|/home/olamundo/domains/olamundo.pl/public_html|/var/www/html|g' wordpress_db.sql

docker cp wordpress/wordpress_db.sql db:/wordpress_db.sql

docker exec -it db bash
mysql -u wordpress -p wordpress < /wordpress_db.sql

```

```bash

docker exec -it db bash

```
