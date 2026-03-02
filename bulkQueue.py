import csv
import json
import urllib.request
import time,random
from pathlib import Path

QUEUE_API_URL = "http://localhost:8080/queue"
ACTIVEMATCHES_API_URL = "http://localhost:8080/active-matches"
REPORT_API_URL = "http://localhost:8080/report"
CSV_FILE = "glickplayers100diff.csv"

players_sent =0


def post_player(player_id=""):
    data = {
        "uid": player_id,
    }

    req = urllib.request.Request(
        QUEUE_API_URL,
        data=json.dumps(data).encode("utf-8"),
        headers={"Content-Type": "application/json"},
        method="POST",
    )

    with urllib.request.urlopen(req) as response:
        return response.read()

def submit_result(winner , match_id):
    d = { "match_id" : match_id , "winner_id" : winner}
    req = urllib.request.Request(
        REPORT_API_URL,
        data=json.dumps(d).encode("utf-8"),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(req) as response:
        return json.loads(response.read())
def csvSource():
    with open(CSV_FILE, newline="") as file:
        reader = csv.reader(file)
        global players_sent
        
        for row in reader:
            if len(row) < 1:
                continue

            
            player_id = row[0].strip('"')

            try:
                post_player(player_id)
                print(f"Inserted {player_id}")
                players_sent += 1
            except Exception as e:
                print(f"Error inserting {player_id}: {e}")
            time.sleep(0.05)

def autoSource(cnt= 200):
    global players_sent
    
    for i in range(cnt):
        try:
            post_player(None)
            players_sent += 1
        except Exception as e:
            print(f"Exception occurred: {e}")
        time.sleep(0.05)
    

def main():
    if (Path(CSV_FILE).exists()): csvSource()
    else: autoSource()
                
    time.sleep(2)
    # after all players are sent
    req = urllib.request.Request(ACTIVEMATCHES_API_URL)
    time.sleep(1)
    
    with urllib.request.urlopen(req) as response:
        data = response.read()
        active_matches = json.loads(data)
        print(f"api found {len(active_matches)} matches")
        for match_id, match in active_matches.items():
            
            player1 = match["Player1"]["ID"]
            player2 = match["Player2"]["ID"]

            winner = random.choice([player1, player2])
            r = submit_result(winner , match_id)
            print(r)
            time.sleep(0.25)
            
            
            


if __name__ == "__main__":
    players_sent = 0
    print("Needs a csv file of player-ids")
    try:
        main()
    except Exception as e:
        print(e)
        print(f"total players queued: {players_sent}")