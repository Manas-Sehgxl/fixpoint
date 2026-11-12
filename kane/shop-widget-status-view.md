# Filing a problem from the shop widget shows a status view

Start URL: http://localhost:4000
Needs: the demo shop running, and Fixpoint started with --stubs. This check files a real
bug report, so it spends a Kane run and an agent call unless the runners are simulated.

Go to http://localhost:4000. Click the "Report a problem" button. Type "The Total line did not change when I removed a pouch." into the "What went wrong?" field. Click the "Send the report" button. Store the text of the panel's heading as 'observed'. Verify the panel shows the four step labels "Received", "Reproduced", "Fix tested" and "Fixed".
