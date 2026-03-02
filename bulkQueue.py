import csv
import json
import sys
import urllib.request
import time,random

QUEUE_API_URL = "http://localhost:8080/queue"
ACTIVEMATCHES_API_URL = "http://localhost:8080/active-matches"
REPORT_API_URL = "http://localhost:8080/report"
CSV_FILE = "glickplayers100diff.csv"
players_sent =0


def post_player(player_id, rating, rd, volatility):
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
        QUEUE_API_URL,
        data=json.dumps(d).encode("utf-8"),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(req) as response:
        return response.read()
    
def main():
    with open(CSV_FILE, newline="") as file:
        reader = csv.reader(file)
        global players_sent
        
        for row in reader:
            if len(row) < 4:
                continue

            if players_sent > 40:
                break
            
            player_id = row[0].strip('"')
            rating = row[1].strip('"')
            rd = row[2].strip('"')
            volatility = row[3].strip('"')

            try:
                post_player(player_id, rating, rd, volatility)
                print(f"Inserted {player_id}")
                players_sent += 1
            except Exception as e:
                print(f"Error inserting {player_id}: {e}")
                
            sleepTime = random.randint(5,50)
            time.sleep(sleepTime / 50)

    # after all players are sent
    req = urllib.request.Request(ACTIVEMATCHES_API_URL)
    with urllib.request.urlopen(req) as response:
        data = response.read()
        active_matches = json.loads(data)
        for match_id, match in active_matches.items():
            
            player1 = match["Player1"]
            player2 = match["Player2"]

            winner = random.choice([player1, player2])
            r = submit_result(winner , match_id)
            print(r)
            time.sleep(0.25)
            
            
            


if __name__ == "__main__":
    players_sent = 0
    try:
        main()
    except:
        print(f"total players queued: {players_sent}")