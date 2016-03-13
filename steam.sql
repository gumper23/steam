create table game
(
    id serial not null
    , app_id int not null
    , name varchar(255) not null
    , playtime_forever int not null
    , first_observed_at timestamp not null default now()
    , constraint id_pk primary key (id)
);
create unique index game_app_id_idx on game (app_id);
create index game_name on game (name);
create index game_playtime_forever on game (playtime_forever);
